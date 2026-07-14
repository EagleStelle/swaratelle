"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getAuthSession,
  login,
  logout,
  updateCredentials,
  type AuthSession,
} from "@/lib/auth-api";

export const AUTH_SESSION_KEY = ["auth", "session"] as const;

const LOGGED_OUT: AuthSession = { authenticated: false, username: "" };

/**
 * Reads the current login state. Session changes only through the mutations
 * below (which write the cache directly), so this never needs to poll.
 */
export function useAuthSession() {
  return useQuery({
    queryKey: AUTH_SESSION_KEY,
    queryFn: getAuthSession,
    staleTime: Infinity,
    retry: false,
  });
}

export function useLogin() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: login,
    onSuccess: (session) => queryClient.setQueryData(AUTH_SESSION_KEY, session),
  });
}

export function useLogout() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: logout,
    onSettled: () => {
      queryClient.setQueryData(AUTH_SESSION_KEY, LOGGED_OUT);
      // Drop cached app data so the next user doesn't briefly see the last one's
      // downloads/history before refetch.
      queryClient.removeQueries({ queryKey: ["downloads"] });
      queryClient.removeQueries({ queryKey: ["history"] });
    },
  });
}

export function useUpdateCredentials() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: updateCredentials,
    onSuccess: (session) => queryClient.setQueryData(AUTH_SESSION_KEY, session),
  });
}
