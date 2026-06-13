import type { Metadata } from "next";

import "./globals.css";
import { NavSide } from "@/components/nav-side";
import { NavBottom } from "@/components/nav-bottom";
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
          <div className="flex min-h-screen">
            <NavSide />
            <main className="flex min-w-0 flex-1 flex-col bg-slate-100 p-4 pb-0 dark:bg-background">
              {children}
            </main>
          </div>
          <NavBottom />
        </Providers>
      </body>
    </html>
  );
}
