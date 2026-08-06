import type { SVGProps } from "react";

type IconProps = SVGProps<SVGSVGElement>;

const defaults = { width: 20, height: 20, viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: 1.8 };

export function RouteIcon(props: IconProps) {
  return <svg {...defaults} {...props}><circle cx="6" cy="18" r="2"/><circle cx="18" cy="6" r="2"/><path d="M8 18h2.3a3 3 0 0 0 3-3v-6a3 3 0 0 1 3-3H16"/></svg>;
}

export function PinIcon(props: IconProps) {
  return <svg {...defaults} {...props}><path d="M20 10c0 5-8 11-8 11S4 15 4 10a8 8 0 1 1 16 0Z"/><circle cx="12" cy="10" r="2.5"/></svg>;
}

export function CrosshairIcon(props: IconProps) {
  return <svg {...defaults} {...props}><circle cx="12" cy="12" r="7"/><circle cx="12" cy="12" r="2"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3"/></svg>;
}

export function PlusIcon(props: IconProps) {
  return <svg {...defaults} {...props}><path d="M12 5v14M5 12h14"/></svg>;
}

export function XIcon(props: IconProps) {
  return <svg {...defaults} {...props}><path d="m6 6 12 12M18 6 6 18"/></svg>;
}

export function SwapIcon(props: IconProps) {
  return <svg {...defaults} {...props}><path d="M7 7h11l-3-3M17 17H6l3 3M18 7l-3 3M6 17l3-3"/></svg>;
}

export function ClockIcon(props: IconProps) {
  return <svg {...defaults} {...props}><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3.5 2"/></svg>;
}

export function ArrowIcon(props: IconProps) {
  return <svg {...defaults} {...props}><path d="M5 12h14M14 7l5 5-5 5"/></svg>;
}

export function CheckIcon(props: IconProps) {
  return <svg {...defaults} {...props}><path d="m5 12 4 4L19 6"/></svg>;
}

export function AlertIcon(props: IconProps) {
  return <svg {...defaults} {...props}><path d="M10.3 4.2 2.7 18a2 2 0 0 0 1.8 3h15a2 2 0 0 0 1.8-3L13.7 4.2a2 2 0 0 0-3.4 0Z"/><path d="M12 9v4M12 17h.01"/></svg>;
}

export function InfoIcon(props: IconProps) {
  return <svg {...defaults} {...props}><circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7h.01"/></svg>;
}

export function GlobeIcon(props: IconProps) {
  return <svg {...defaults} {...props}><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a15 15 0 0 1 0 18M12 3a15 15 0 0 0 0 18"/></svg>;
}

export function ShieldIcon(props: IconProps) {
  return <svg {...defaults} {...props}><path d="M12 3 5 6v5c0 4.7 2.8 8 7 10 4.2-2 7-5.3 7-10V6l-7-3Z"/><path d="m9 12 2 2 4-5"/></svg>;
}

export function ActivityIcon(props: IconProps) {
  return <svg {...defaults} {...props}><path d="M3 12h4l2-7 4 14 2-7h6"/></svg>;
}

export function ChevronIcon(props: IconProps) {
  return <svg {...defaults} {...props}><path d="m8 10 4 4 4-4"/></svg>;
}

export function BookmarkIcon(props: IconProps) {
  return <svg {...defaults} {...props}><path d="M7 4h10a1 1 0 0 1 1 1v15l-6-3.6L6 20V5a1 1 0 0 1 1-1Z"/></svg>;
}

export function SunIcon(props: IconProps) {
  return <svg {...defaults} {...props}><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>;
}

export function MoonIcon(props: IconProps) {
  return <svg {...defaults} {...props}><path d="M20 14.5A8.5 8.5 0 0 1 9.5 4a8.5 8.5 0 1 0 10.5 10.5Z"/></svg>;
}

export function MenuIcon(props: IconProps) {
  return <svg {...defaults} {...props}><path d="M4 7h16M4 12h16M4 17h16"/></svg>;
}

export function RefreshIcon(props: IconProps) {
  return <svg {...defaults} {...props}><path d="M20 7v5h-5M4 17v-5h5"/><path d="M6.1 9a7 7 0 0 1 11.7-2L20 9M4 15l2.2 2a7 7 0 0 0 11.7-2"/></svg>;
}
