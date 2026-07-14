import type { Metadata } from "next";

import "./globals.css";
import { AuthGate } from "@/components/auth/auth-gate";
import { ThemeFavicon } from "@/components/theme-favicon";
import { Providers } from "./providers";

export const metadata: Metadata = {
  title: "Swaratelle",
  description: "Iwara-DL Web Interface",
  icons: {
    icon: [{ url: "/logo.svg", type: "image/svg+xml" }],
    shortcut: [{ url: "/logo.svg", type: "image/svg+xml" }],
  },
};

const themeScript = `
(function () {
  try {
    var t = localStorage.getItem("swaratelle-theme");
    if (!t) t = window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
    if (t === "dark") document.documentElement.classList.add("dark");
  } catch (e) {}
})();
`;

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
      </head>
      <body>
        <Providers>
          <ThemeFavicon />
          <AuthGate>{children}</AuthGate>
        </Providers>
      </body>
    </html>
  );
}
