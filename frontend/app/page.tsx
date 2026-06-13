"use client";

import { useState } from "react";
import {
  CircleAlert,
  ClipboardPaste,
  Download,
  Info,
  Link,
  Trash2,
  X,
} from "lucide-react";

import { AlertDock } from "@/components/alert-dock";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import type { QueueResult } from "@/lib/api";
import {
  useActiveDownloads,
  useQueueDownloads,
  useRemoveDownload,
} from "@/lib/hooks";
import { displayName } from "@/lib/format";

type QueueFeedback = {
  title: string;
  description: string;
  icon: "info" | "error";
  variant?: "default" | "destructive";
};

function getQueueFeedback(result?: QueueResult): QueueFeedback | null {
  if (!result) return null;

  switch (result.status) {
    case "queued":
      return null;
    case "done":
      return {
        title: "Already downloaded",
        description: "It is already in history.",
        icon: "info",
      };
    case "pending":
      return {
        title: "Already queued",
        description: "It is waiting in downloads.",
        icon: "info",
      };
    case "downloading":
      return {
        title: "Already downloading",
        description: "Progress is shown below.",
        icon: "info",
      };
    case "rejected":
      return {
        title: "Link not recognized",
        description: "Paste a valid Iwara video URL.",
        icon: "error",
        variant: "destructive",
      };
    case "failed":
      return {
        title: "Could not queue",
        description: "Try again in a moment.",
        icon: "error",
        variant: "destructive",
      };
    default:
      return {
        title: "Queue updated",
        description: `Status: ${result.status}.`,
        icon: "info",
      };
  }
}

export default function DownloadsPage() {
  const [url, setUrl] = useState("");
  const queue = useQueueDownloads();
  const downloads = useActiveDownloads();
  const removeDownload = useRemoveDownload();

  const active = downloads.data ?? [];
  const queueFeedback = getQueueFeedback(
    queue.isSuccess ? queue.data?.[0] : undefined,
  );

  function submit() {
    const val = url.trim();
    if (!val) return;
    queue.mutate([val], { onSuccess: () => setUrl("") });
  }

  async function paste() {
    try {
      const text = await navigator.clipboard.readText();
      if (text) setUrl(text.trim());
    } catch {
      // clipboard read blocked (no permission / insecure context)
    }
  }

  return (
    <div className="flex flex-1 flex-col gap-4">
      <AlertDock elevated>
        {queue.isError && (
          <Alert variant="destructive">
            <CircleAlert />
            <AlertTitle>Could not queue</AlertTitle>
            <AlertDescription>Check the server and try again.</AlertDescription>
          </Alert>
        )}

        {queueFeedback && (
          <Alert variant={queueFeedback.variant}>
            {queueFeedback.icon === "error" ? <CircleAlert /> : <Info />}
            <AlertTitle>{queueFeedback.title}</AlertTitle>
            <AlertDescription>{queueFeedback.description}</AlertDescription>
          </Alert>
        )}
      </AlertDock>

      <div className="flex flex-col gap-2">
        {active.map((r) => {
          const failed = Boolean(r.Error.trim());
          const title = displayName(r);
          const removing = removeDownload.variables === r.VideoID;

          return (
            <Card key={r.VideoID} className="gap-3 py-4">
              <CardHeader className="grid-cols-[minmax(0,1fr)_2.25rem] grid-rows-[auto] items-center gap-x-3 gap-y-0 has-data-[slot=card-action]:grid-cols-[minmax(0,1fr)_2.25rem]">
                <CardTitle className="min-w-0 truncate leading-snug">
                  {title}
                </CardTitle>
                <CardAction className="row-span-1 self-center justify-self-end">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    onClick={() => removeDownload.mutate(r.VideoID)}
                    disabled={removing && removeDownload.isPending}
                    aria-label={`${failed ? "Remove" : "Cancel"} ${title}`}
                    className="size-8 text-destructive hover:text-destructive"
                  >
                    {failed ? <Trash2 /> : <X />}
                  </Button>
                </CardAction>
              </CardHeader>
              <CardContent className="grid grid-cols-[minmax(0,1fr)_2.25rem] items-center gap-x-3 gap-y-2">
                <a
                  href={r.SourceURL}
                  target="_blank"
                  rel="noreferrer"
                  className="col-span-2 min-w-0 truncate text-sm text-muted-foreground hover:underline"
                >
                  {r.SourceURL}
                </a>
                {failed ? (
                  <div className="col-span-2 rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
                    <div className="flex items-center gap-2 font-medium">
                      <CircleAlert className="size-4" />
                      Download failed
                    </div>
                    <p className="mt-1 whitespace-pre-wrap break-words text-xs">
                      {r.Error}
                    </p>
                  </div>
                ) : (
                  <>
                    <div className="h-1.5 min-w-0 overflow-hidden rounded-full bg-secondary">
                      <div
                        className="h-full rounded-full bg-primary transition-[width] duration-500"
                        style={{ width: `${r.Progress}%` }}
                      />
                    </div>
                    <span className="w-9 justify-self-end text-right text-xs tabular-nums text-muted-foreground">
                      {r.Progress}%
                    </span>
                  </>
                )}
              </CardContent>
            </Card>
          );
        })}
      </div>

      <div className="order-last sticky bottom-16 z-40 -mx-4 mt-auto flex items-center gap-2 border-t bg-slate-100 p-4 dark:bg-background md:static md:bottom-auto md:order-first md:mx-0 md:mt-0 md:border-0 md:bg-transparent md:p-0 md:dark:bg-transparent">
        <div className="relative flex-1">
          <Link className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="url"
            className="bg-background pl-8 pr-10"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") submit();
            }}
          />
          <Button
            type="button"
            variant="ghost"
            size="icon"
            onClick={paste}
            aria-label="Paste from clipboard"
            className="absolute right-1 top-1/2 size-7 -translate-y-1/2"
          >
            <ClipboardPaste />
          </Button>
        </div>
        <Button
          size="icon"
          onClick={submit}
          disabled={queue.isPending || !url.trim()}
          aria-label="Download"
        >
          <Download />
        </Button>
      </div>
    </div>
  );
}
