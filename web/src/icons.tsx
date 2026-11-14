// Small original line icons, drawn on a 24px grid.
const paths: Record<string, string> = {
  home: "M3 11l9-7 9 7M5 10v10h5v-6h4v6h5V10",
  globe: "M12 3a9 9 0 100 18 9 9 0 000-18zM3 12h18M12 3c2.5 2.6 3.8 5.6 3.8 9S14.5 18.4 12 21c-2.5-2.6-3.8-5.6-3.8-9S9.5 5.6 12 3z",
  lock: "M6 11h12v9H6zM8.5 11V8a3.5 3.5 0 017 0v3M12 15v2",
  code: "M8 8l-4 4 4 4M16 8l4 4-4 4M13.5 5l-3 14",
  database: "M5 6c0-1.7 3.1-3 7-3s7 1.3 7 3-3.1 3-7 3-7-1.3-7-3zM5 6v6c0 1.7 3.1 3 7 3s7-1.3 7-3V6M5 12v6c0 1.7 3.1 3 7 3s7-1.3 7-3v-6",
  users: "M9 11a3.5 3.5 0 100-7 3.5 3.5 0 000 7zM2.5 20c.500-3.3 3-5.5 6.5-5.5s6 2.2 6.5 5.5M16 4.5a3.5 3.5 0 010 6.5M18 14.8c2 .700 3.2 2.6 3.5 5.2",
  folder: "M3 6.5A1.5 1.5 0 014.5 5H9l2 2.5h8.5A1.5 1.5 0 0121 9v9.5a1.5 1.5 0 01-1.5 1.5h-15A1.5 1.5 0 013 18.5z",
  transfer: "M7 4v13M7 4L3.5 7.5M7 4l3.5 3.5M17 20V7M17 20l-3.5-3.5M17 20l3.5-3.5",
  mail: "M3 6h18v12H3zM3 7l9 6.5L21 7",
  forward: "M3 6h12v5M3 6v12h9M3 7l6 4.5L15 7M15 17h6M18.5 14l3 3-3 3",
  network: "M9.5 3h5v4h-5zM3 17h5v4H3zM16 17h5v4h-5zM12 7v5M5.5 17v-3h13v3M12 12v2",
  server: "M4 4h16v6H4zM4 14h16v6H4zM7.5 7h.01M7.5 17h.01M11 7h6M11 17h6",
  activity: "M3 12h4l2.5-7 4 14 2.5-7H21",
  settings: "M4 7h9M17 7h3M4 17h3M11 17h9M15 4.5a2.5 2.5 0 100 5 2.5 2.5 0 000-5zM9 14.5a2.5 2.5 0 100 5 2.5 2.5 0 000-5z",
  key: "M14.5 9.5a4.5 4.5 0 11-3.7 7L4 20H3v-3l1.5-1.5H6V14h1.5l2.3-2.3A4.5 4.5 0 0114.5 9.5zM16 13h.01",
  search: "M10.5 4a6.5 6.5 0 100 13 6.5 6.5 0 000-13zM15.5 15.5L20 20",
  chevron: "M6 9l6 6 6-6",
  menu: "M4 6h16M4 12h16M4 18h16",
  logout: "M10 4H5v16h5M14 8l4 4-4 4M18 12H9",
  puzzle: "M9 4h6v3.2a2 2 0 102.8 2.8H21v6h-3.2a2 2 0 10-2.8 2.8V20H9v-1.2a2 2 0 10-2.8-2.8H3v-6h3.2A2 2 0 109 7.2z",
  archive: "M3 5h18v4H3zM5 9v10h14V9M9.5 13h5",
  clock: "M12 3a9 9 0 100 18 9 9 0 000-18zM12 7.5V12l3 2",
  shield: "M12 3l8 3v6c0 4.5-3.2 7.7-8 9-4.8-1.3-8-4.5-8-9V6zM8.5 12l2.5 2.5 4.5-5",
};

export function Icon({ name, size = 20 }: { name: string; size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d={paths[name] || paths.home} />
    </svg>
  );
}
