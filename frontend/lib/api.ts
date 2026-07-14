// The UI is served by the same Go service that exposes these endpoints, so all
// requests are same-origin and need no auth header -- the bundled appliance
// handles that itself.
export const API_BASE = "";

export interface DownloadRecord {
  VideoID: string;
  SourceURL: string;
  FilePath: string;
  FileSize: number;
  Title: string;
  Artist: string;
  Error: string;
  Attempts: number;
  CreatedAt: number;
  UpdatedAt: number;
  // Live download percent (0-100) for active downloads.
  Progress: number;
}

export interface HistoryResponse {
  records: DownloadRecord[];
  next_cursor?: string;
}

export interface QueueResult {
  url: string;
  video_id?: string;
  status: string;
  error?: string;
}

export async function fetchActiveDownloads(): Promise<DownloadRecord[]> {
  const res = await fetch(`${API_BASE}/api/downloads/active`);
  if (!res.ok) {
    throw new Error(`active downloads request failed: ${res.status}`);
  }
  return ((await res.json()) as DownloadRecord[] | null) ?? [];
}

export async function fetchHistoryPage({
  limit,
  cursor,
  query,
}: {
  limit: number;
  cursor?: string;
  query?: string;
}): Promise<HistoryResponse> {
  const params = new URLSearchParams({ limit: String(limit) });
  if (cursor) params.set("cursor", cursor);
  if (query?.trim()) params.set("q", query.trim());

  const res = await fetch(`${API_BASE}/api/history?${params.toString()}`);
  if (!res.ok) {
    throw new Error(`history request failed: ${res.status}`);
  }

  const data = (await res.json()) as HistoryResponse | null;
  return {
    records: data?.records ?? [],
    next_cursor: data?.next_cursor,
  };
}

export interface ScanResult {
  checked: number;
  missing: number;
  added: number;
}

export async function scanDownloads(): Promise<ScanResult> {
  const res = await fetch(`${API_BASE}/api/scan`, { method: "POST" });
  if (!res.ok) {
    throw new Error(`scan request failed: ${res.status}`);
  }
  return (await res.json()) as ScanResult;
}

export async function removeDownload(videoID: string): Promise<void> {
  const res = await fetch(
    `${API_BASE}/api/downloads/${encodeURIComponent(videoID)}`,
    { method: "DELETE" },
  );
  if (!res.ok) {
    throw new Error(`remove download request failed: ${res.status}`);
  }
}

// Same-origin URL of the completed media file for a video id. The UI's session
// cookie (or the bearer token, for external clients like never-stelle) authorizes
// it; only "done" records resolve to bytes, everything else returns 404/409.
export function downloadFileURL(videoID: string): string {
  return `${API_BASE}/api/downloads/${encodeURIComponent(videoID)}/file`;
}

export async function queueDownloads(urls: string[]): Promise<QueueResult[]> {
  const res = await fetch(`${API_BASE}/api/queue`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ urls }),
  });
  if (!res.ok) {
    throw new Error(`queue request failed: ${res.status}`);
  }
  return ((await res.json()) as QueueResult[] | null) ?? [];
}
