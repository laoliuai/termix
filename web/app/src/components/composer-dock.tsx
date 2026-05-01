import type { ComponentChildren } from "preact";

export interface ComposerDockProps {
  open: boolean;
  children: ComponentChildren;
}

// Wraps composer + toolbar so the terminal page can mount/unmount the
// input chrome with a max-height transition. Children render only when
// open is true; the wrapper stays in the DOM for a stable landmark and so
// the transition can animate.
export function ComposerDock({ open, children }: ComposerDockProps) {
  return (
    <div class={`composer-dock${open ? " is-open" : ""}`} aria-hidden={!open}>
      {open ? children : null}
    </div>
  );
}
