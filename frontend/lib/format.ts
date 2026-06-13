import type { DownloadRecord } from "@/lib/api";

export function formatSize(bytes: number): string {
  if (!bytes) return "-";
  return `${(bytes / 1048576).toFixed(1)} MB`;
}

// Best available "Name": iwaradl's title once known (captured mid-download or
// on completion), otherwise the slug from the source URL, with the video id as
// a last resort. Extensions are stripped for display.
export function displayName(r: DownloadRecord): string {
  if (r.Title) return r.Title;

  const file = r.FilePath?.split(/[\\/]/).pop();
  if (file) return file.replace(/\.[^.]+$/, "");

  // URL shape: https://www.iwara.tv/video/<id>/<slug>
  const slug = r.SourceURL?.match(/\/video\/[^/]+\/([^/?#]+)/)?.[1];
  if (slug) return decodeURIComponent(slug).replace(/-/g, " ");

  return r.VideoID;
}
