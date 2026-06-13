"use client";

import { Suspense, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  ChevronDown,
  CircleAlert,
  CircleCheck,
  RefreshCw,
  Search,
  X,
} from "lucide-react";

import { AlertDock } from "@/components/alert-dock";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { ScanResult } from "@/lib/api";
import { useHistoryDownloads, useScan } from "@/lib/hooks";
import { displayName, formatSize } from "@/lib/format";

function pluralize(count: number, singular: string, plural = `${singular}s`) {
  return `${count} ${count === 1 ? singular : plural}`;
}

function scanTitle(result: ScanResult) {
  return result.missing > 0 || result.added > 0
    ? "History updated"
    : "Scan complete";
}

function scanDescription(result: ScanResult) {
  const parts = [`Checked ${pluralize(result.checked, "file")}.`];

  if (result.missing > 0) {
    parts.push(`Removed ${pluralize(result.missing, "missing item")}.`);
  } else {
    parts.push("Nothing missing.");
  }

  if (result.added > 0) {
    parts.push(`Added ${pluralize(result.added, "file")} from disk.`);
  }

  return parts.join(" ");
}

export default function HistoryPage() {
  return (
    <Suspense fallback={<HistoryFallback />}>
      <HistoryContent />
    </Suspense>
  );
}

function HistoryFallback() {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-end gap-2">
        <Button variant="outline" size="icon" aria-label="Scan" disabled>
          <RefreshCw />
        </Button>
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Artist</TableHead>
            <TableHead>URL</TableHead>
            <TableHead>Size</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow>
            <TableCell colSpan={4}>Loading...</TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
  );
}

function HistoryContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const activeQuery = searchParams.get("q")?.trim() ?? "";
  const [searchValue, setSearchValue] = useState(activeQuery);
  const history = useHistoryDownloads(activeQuery);
  const scan = useScan();
  const items = history.data?.pages.flatMap((page) => page.records) ?? [];

  useEffect(() => {
    setSearchValue(activeQuery);
  }, [activeQuery]);

  function pushSearch(nextQuery: string) {
    const params = new URLSearchParams(searchParams.toString());
    const trimmed = nextQuery.trim();

    if (trimmed) {
      params.set("q", trimmed);
    } else {
      params.delete("q");
    }

    const nextPath = params.toString()
      ? `/history?${params.toString()}`
      : "/history";
    router.push(nextPath);
  }

  function submitSearch(nextValue = searchValue) {
    const trimmed = nextValue.trim();
    setSearchValue(trimmed);
    if (trimmed === activeQuery) return;
    pushSearch(trimmed);
  }

  function clearSearch() {
    setSearchValue("");
    if (activeQuery) pushSearch("");
  }

  return (
    <div className="flex flex-1 flex-col gap-4">
      <AlertDock elevated>
        {scan.isError && (
          <Alert variant="destructive">
            <CircleAlert />
            <AlertTitle>Scan failed</AlertTitle>
            <AlertDescription>
              Could not check history. Try again.
            </AlertDescription>
          </Alert>
        )}
        {scan.data && (
          <Alert>
            <CircleCheck />
            <AlertTitle>{scanTitle(scan.data)}</AlertTitle>
            <AlertDescription>{scanDescription(scan.data)}</AlertDescription>
          </Alert>
        )}
      </AlertDock>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Artist</TableHead>
            <TableHead>URL</TableHead>
            <TableHead>Size</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {history.isLoading || (history.isError && items.length === 0) ? (
            <TableRow>
              <TableCell colSpan={4}>
                {history.isLoading ? "Loading..." : "Error loading history."}
              </TableCell>
            </TableRow>
          ) : (
            items.map((r) => (
              <TableRow key={r.VideoID}>
                <TableCell className="max-w-[16rem] truncate">
                  {displayName(r)}
                </TableCell>
                <TableCell>{r.Artist || "-"}</TableCell>
                <TableCell className="max-w-[20rem] truncate">
                  <a
                    href={r.SourceURL}
                    target="_blank"
                    rel="noreferrer"
                    className="hover:underline"
                  >
                    {r.SourceURL}
                  </a>
                </TableCell>
                <TableCell>{formatSize(r.FileSize)}</TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>

      {history.hasNextPage && (
        <div className="flex justify-center">
          <Button
            variant="outline"
            size="sm"
            onClick={() => history.fetchNextPage()}
            disabled={history.isFetchingNextPage}
          >
            <ChevronDown />
            {history.isFetchingNextPage ? "Loading..." : "Load more"}
          </Button>
        </div>
      )}

      <div className="order-last sticky bottom-16 z-40 -mx-4 mt-auto flex items-center gap-2 border-t bg-slate-100 p-4 dark:bg-background md:static md:bottom-auto md:order-first md:mx-0 md:mt-0 md:border-0 md:bg-transparent md:p-0 md:dark:bg-transparent">
        <div className="relative flex-1">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="text"
            role="searchbox"
            enterKeyHint="search"
            value={searchValue}
            onChange={(event) => setSearchValue(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                submitSearch(event.currentTarget.value);
              }
            }}
            aria-label="Search history"
            className="bg-background pl-8 pr-9"
          />
          {searchValue && (
            <Button
              type="button"
              variant="ghost"
              size="icon"
              onClick={clearSearch}
              aria-label="Clear search"
              className="absolute right-1 top-1/2 size-7 -translate-y-1/2"
            >
              <X />
            </Button>
          )}
        </div>
        <Button
          variant="outline"
          size="icon"
          onClick={() => scan.mutate()}
          disabled={scan.isPending}
          aria-label="Scan"
        >
          <RefreshCw className={scan.isPending ? "animate-spin" : undefined} />
        </Button>
      </div>
    </div>
  );
}
