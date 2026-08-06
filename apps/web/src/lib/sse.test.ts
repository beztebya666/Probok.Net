import { afterEach, describe, expect, it, vi } from "vitest";
import { SseParser, streamSearchEvents } from "./sse";

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("SseParser", () => {
  it("parses fragmented named events and multiline data", () => {
    const parser = new SseParser();
    expect(parser.feed("id: 41\nevent: PROG")).toEqual([]);
    expect(parser.feed("RESS\ndata: {\"part\":\ndata: \"two\"}\nretry: 1500\n\n")).toEqual([
      { id: "41", event: "PROGRESS", data: "{\"part\":\n\"two\"}", retry: 1500 },
    ]);
  });

  it("ignores heartbeat comments and rejects NUL event ids", () => {
    const parser = new SseParser();
    expect(parser.feed(": heartbeat\nid: bad\0id\ndata: {}\n\n")).toEqual([{ data: "{}" }]);
  });

  it("flushes a final event when the stream closes without a blank line", () => {
    const parser = new SseParser();
    parser.feed("id: 9\ndata: {\"ok\":true}");
    expect(parser.finish()).toEqual([{ id: "9", data: "{\"ok\":true}" }]);
  });

	it("rejects an unbounded line or event before memory can grow indefinitely", () => {
		const lineParser = new SseParser();
		expect(() => lineParser.feed("x".repeat(256 * 1024 + 1))).toThrow("SSE_LINE_TOO_LARGE");

		const eventParser = new SseParser();
		const dataLine = `data: ${"x".repeat(200_000)}\n`;
		expect(() => eventParser.feed(dataLine.repeat(6))).toThrow("SSE_EVENT_TOO_LARGE");
	});

  it("reconnects with Last-Event-ID and accepts the numeric backend event id", async () => {
    vi.stubEnv("NEXT_PUBLIC_DEMO_MODE", "false");
    vi.stubEnv("NEXT_PUBLIC_EDGE_API_BASE_URL", "https://edge.greenroute.example");
    const body = (text: string) => new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode(text));
        controller.close();
      },
    });
    const timestamp = "2026-08-05T12:00:00.000Z";
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(body(`id: 1\nevent: SEARCH_ACCEPTED\ndata: {"eventId":1,"searchId":"search-1","type":"SEARCH_ACCEPTED","timestamp":"${timestamp}"}\n\n`), {
        headers: { "Content-Type": "text/event-stream" },
      }))
      .mockResolvedValueOnce(new Response(body(`id: 2\nevent: SEARCH_COMPLETED\ndata: {"eventId":2,"searchId":"search-1","type":"SEARCH_COMPLETED","timestamp":"${timestamp}"}\n\n`), {
        headers: { "Content-Type": "text/event-stream" },
      }));
    vi.stubGlobal("fetch", fetchMock);
    const received: string[] = [];

    await streamSearchEvents(
      "search-1",
      new AbortController().signal,
      (event) => {
        received.push(event.type);
        return event.type === "SEARCH_COMPLETED";
      },
      () => undefined,
    );

    expect(received).toEqual(["SEARCH_ACCEPTED", "SEARCH_COMPLETED"]);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    const secondInit = fetchMock.mock.calls[1]?.[1] as RequestInit;
    expect(new Headers(secondInit.headers).get("Last-Event-ID")).toBe("1");
  });
});
