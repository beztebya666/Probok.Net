import { playDemoEvents } from "./demo-fixtures";
import { getRuntimeConfig } from "./runtime-config";
import { parseError, routeEventsRequest } from "./api-client";
import { SearchEventSchema, type SearchEvent } from "./schemas";

type RawSseEvent = { id?: string; event?: string; data: string; retry?: number };
const MAX_SSE_LINE_CHARS = 256 * 1024;
const MAX_SSE_EVENT_CHARS = 1024 * 1024;

export class SseParser {
  private buffer = "";
  private event: { id?: string; event?: string; data: string[]; retry?: number } = { data: [] };
  private eventChars = 0;

  feed(chunk: string): RawSseEvent[] {
    this.buffer += chunk.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
    const events: RawSseEvent[] = [];
    let newline = this.buffer.indexOf("\n");
    while (newline >= 0) {
      if (newline > MAX_SSE_LINE_CHARS) throw new Error("SSE_LINE_TOO_LARGE");
      const line = this.buffer.slice(0, newline);
      this.buffer = this.buffer.slice(newline + 1);
      const event = this.consumeLine(line);
      if (event) events.push(event);
      newline = this.buffer.indexOf("\n");
    }
    if (this.buffer.length > MAX_SSE_LINE_CHARS) throw new Error("SSE_LINE_TOO_LARGE");
    return events;
  }

  finish(): RawSseEvent[] {
    const events: RawSseEvent[] = [];
    if (this.buffer) {
      const event = this.consumeLine(this.buffer);
      if (event) events.push(event);
      this.buffer = "";
    }
    const finalEvent = this.consumeLine("");
    if (finalEvent) events.push(finalEvent);
    return events;
  }

  private consumeLine(line: string): RawSseEvent | undefined {
    if (line === "") {
      if (!this.event.data.length) {
        this.event = { data: [] };
        this.eventChars = 0;
        return undefined;
      }
      const complete: RawSseEvent = {
        data: this.event.data.join("\n"),
        ...(this.event.id ? { id: this.event.id } : {}),
        ...(this.event.event ? { event: this.event.event } : {}),
        ...(this.event.retry ? { retry: this.event.retry } : {}),
      };
      this.event = { data: [] };
      this.eventChars = 0;
      return complete;
    }
    if (line.startsWith(":")) return undefined;
    const separator = line.indexOf(":");
    const field = separator < 0 ? line : line.slice(0, separator);
    const value = separator < 0 ? "" : line.slice(separator + 1).replace(/^ /, "");
    if (field === "data") {
      this.eventChars += value.length + 1;
      if (this.eventChars > MAX_SSE_EVENT_CHARS) throw new Error("SSE_EVENT_TOO_LARGE");
      this.event.data.push(value);
    }
    if (field === "id" && !value.includes("\0")) this.event.id = value;
    if (field === "event") this.event.event = value;
    if (field === "retry" && /^\d+$/.test(value)) this.event.retry = Number(value);
    return undefined;
  }
}

function wait(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(resolve, ms);
    signal.addEventListener(
      "abort",
      () => {
        window.clearTimeout(timer);
        reject(new DOMException("Aborted", "AbortError"));
      },
      { once: true },
    );
  });
}

function parseEvent(raw: RawSseEvent, searchId: string): SearchEvent | undefined {
  let payload: unknown;
  try {
    payload = JSON.parse(raw.data);
  } catch {
    return undefined;
  }
  if (payload && typeof payload === "object") {
    const mutable = { ...(payload as Record<string, unknown>) };
    if (!mutable.eventId && raw.id) mutable.eventId = raw.id;
    if (!mutable.searchId) mutable.searchId = searchId;
    if (!mutable.type && raw.event) mutable.type = raw.event;
    if (typeof mutable.progress !== "number" && mutable.metadata && typeof mutable.metadata === "object") {
      const metadataProgress = (mutable.metadata as Record<string, unknown>).progress;
      if (typeof metadataProgress === "number") mutable.progress = metadataProgress;
    }
    payload = mutable;
  }
  const parsed = SearchEventSchema.safeParse(payload);
  return parsed.success ? parsed.data : undefined;
}

export type StreamState = { attempt: number; lastEventId?: string };

export async function streamSearchEvents(
  searchId: string,
  signal: AbortSignal,
  onEvent: (event: SearchEvent) => boolean | void,
  onReconnect: (state: StreamState) => void,
): Promise<void> {
  if (getRuntimeConfig().demoMode) {
    await playDemoEvents(searchId, signal, (event) => onEvent(event));
    return;
  }

  let lastEventId: string | undefined;
  let reconnectDelay = 700;
  let attempt = 0;

  while (!signal.aborted && attempt < 4) {
    if (attempt > 0) {
      onReconnect({ attempt, ...(lastEventId ? { lastEventId } : {}) });
      await wait(Math.min(8_000, reconnectDelay * 2 ** (attempt - 1)), signal);
    }

    try {
      const request = routeEventsRequest(searchId, lastEventId);
      const response = await fetch(request.url, { ...request.init, signal });
      if (!response.ok) throw await parseError(response);
      if (!response.body) throw new Error("SSE_RESPONSE_BODY_MISSING");
      const contentType = response.headers.get("content-type") ?? "";
      if (!contentType.toLowerCase().includes("text/event-stream")) throw new Error("INVALID_SSE_CONTENT_TYPE");

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      const parser = new SseParser();
      let terminal = false;
      for (;;) {
        const { value, done } = await reader.read();
        const rawEvents = done ? parser.finish() : parser.feed(decoder.decode(value, { stream: !done }));
        for (const raw of rawEvents) {
          if (raw.id) lastEventId = raw.id;
          if (raw.retry) reconnectDelay = Math.min(10_000, Math.max(250, raw.retry));
          const event = parseEvent(raw, searchId);
          if (event) terminal = onEvent(event) === true || terminal;
        }
        if (terminal) {
          await reader.cancel();
          return;
        }
        if (done) break;
      }
      attempt += 1;
    } catch (error) {
      if (signal.aborted || (error instanceof DOMException && error.name === "AbortError")) throw error;
      attempt += 1;
      if (error instanceof Error && "status" in error && [401, 403, 404].includes(Number(error.status))) throw error;
    }
  }
  throw new Error("SSE_RECONNECT_EXHAUSTED");
}
