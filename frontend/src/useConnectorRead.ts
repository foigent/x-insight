import { useCallback, useEffect, useRef, useState } from "react";

type ReadState<T> = { key: string | null; data?: T; error?: unknown; loading: boolean };

// Connector controls depend on a confirmed read. A failed poll or refresh
// discards the previous status, and superseded reads cannot publish a result.
export function useConnectorRead<T>(
  key: string | null,
  load: (signal: AbortSignal) => Promise<T>,
  pollInterval?: number,
  shouldPoll?: (data: T) => boolean,
) {
  const [state, setState] = useState<ReadState<T>>({ key, loading: !!key });
  const request = useRef<AbortController | null>(null);
  const cancel = useCallback(() => request.current?.abort(), []);
  const refresh = useCallback(async () => {
    if (!key) return;
    cancel();
    const controller = new AbortController();
    request.current = controller;
    setState(previous => ({ key, data: previous.key === key ? previous.data : undefined, loading: true }));
    try {
      const data = await load(controller.signal);
      if (!controller.signal.aborted) {
        setState({ key, data, loading: false });
        return data;
      }
    } catch (error) {
      if (!controller.signal.aborted) setState({ key, error, loading: false });
    }
  }, [cancel, key, load]);
  const invalidate = useCallback((error: unknown) => {
    cancel();
    setState({ key, error, loading: false });
  }, [cancel, key]);
  const accept = useCallback((data: T) => {
    cancel();
    setState({ key, data, loading: false });
  }, [cancel, key]);

  useEffect(() => {
    void refresh();
    return cancel;
  }, [cancel, refresh]);
  const { data, error, loading } = key && state.key === key ? state : { data: undefined, error: undefined, loading: !!key };
  useEffect(() => {
    if (pollInterval === undefined || loading || !data || !shouldPoll?.(data)) return;
    const timer = window.setTimeout(() => void refresh(), pollInterval);
    return () => window.clearTimeout(timer);
  }, [data, loading, pollInterval, refresh, shouldPoll]);

  return { data, error, loading, refresh, invalidate, accept };
}
