"use client";

import { useEffect } from "react";

const FAVICON_COLORS = {
  light: "#0f172a",
  dark: "#f8fafc",
} as const;

function getTheme() {
  return document.documentElement.classList.contains("dark") ? "dark" : "light";
}

function getFaviconLink() {
  let link =
    document.querySelector<HTMLLinkElement>(
      'link[rel="icon"][type="image/svg+xml"]'
    ) ?? document.querySelector<HTMLLinkElement>('link[rel="icon"]');

  if (!link) {
    link = document.createElement("link");
    link.rel = "icon";
    document.head.appendChild(link);
  }

  link.type = "image/svg+xml";
  return link;
}

function colorLogoSvg(svg: string, color: string) {
  const style = `<style>.s0 { fill: ${color} }</style>`;

  return svg.includes("<style>")
    ? svg.replace(/<style>[\s\S]*?<\/style>/, style)
    : svg.replace(/(<svg\b[^>]*>)/, `$1${style}`);
}

export function ThemeFavicon() {
  useEffect(() => {
    if (typeof fetch !== "function") return;

    let cancelled = false;
    const logoSvg = fetch("/logo.svg").then((response) => {
      if (!response.ok) {
        throw new Error("Unable to load logo.svg");
      }

      return response.text();
    });

    async function applyFavicon() {
      try {
        const svg = await logoSvg;
        if (cancelled) return;

        const theme = getTheme();
        const link = getFaviconLink();
        const themedSvg = colorLogoSvg(svg, FAVICON_COLORS[theme]);
        link.href = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(
          themedSvg
        )}`;
      } catch {
        if (!cancelled) {
          getFaviconLink().href = "/logo.svg";
        }
      }
    }

    if (typeof MutationObserver === "undefined") {
      applyFavicon();
      return () => {
        cancelled = true;
      };
    }

    const observer = new MutationObserver(applyFavicon);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class"],
    });

    applyFavicon();

    return () => {
      cancelled = true;
      observer.disconnect();
    };
  }, []);

  return null;
}
