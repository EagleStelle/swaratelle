import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  fetchActiveDownloads,
  fetchHistoryPage,
  queueDownloads,
  removeDownload,
  scanDownloads,
} from "@/lib/api";

export const HISTORY_PAGE_SIZE = 50;

export function useActiveDownloads() {
  return useQuery({
    queryKey: ["downloads", "active"],
    queryFn: fetchActiveDownloads,
    refetchInterval: 2000,
  });
}

export function useHistoryDownloads(query = "", limit = HISTORY_PAGE_SIZE) {
  const searchQuery = query.trim();

  return useInfiniteQuery({
    queryKey: ["history", limit, searchQuery],
    queryFn: ({ pageParam }) =>
      fetchHistoryPage({
        limit,
        cursor: pageParam as string | undefined,
        query: searchQuery,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
  });
}

export function useScan() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: scanDownloads,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["history"] });
    },
  });
}

export function useQueueDownloads() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (urls: string[]) => queueDownloads(urls),
    onSuccess: (results) => {
      const activeChanged = results.some((result) =>
        ["queued", "pending", "downloading"].includes(result.status),
      );
      const historyChanged = results.some((result) => result.status === "done");

      if (activeChanged) {
        queryClient.invalidateQueries({ queryKey: ["downloads", "active"] });
      }
      if (historyChanged) {
        queryClient.invalidateQueries({ queryKey: ["history"] });
      }
    },
  });
}

export function useRemoveDownload() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (videoID: string) => removeDownload(videoID),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["downloads", "active"] });
    },
  });
}
