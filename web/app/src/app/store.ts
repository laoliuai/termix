import { signal } from "@preact/signals";

export type SnackKind = "info" | "warn" | "error";

export interface SnackbarState {
  msg: string;
  kind: SnackKind;
  action?: { label: string; cb: () => void };
}

export const snackbar = signal<SnackbarState | null>(null);
export const splashing = signal<boolean>(true);

export function notify(msg: string, kind: SnackKind = "info", action?: SnackbarState["action"]): void {
  snackbar.value = { msg, kind, action };
}
