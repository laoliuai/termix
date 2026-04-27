import { useEffect } from "preact/hooks";
import { signal, type Signal } from "@preact/signals";

/**
 * Returns a Signal whose value is the height of the soft keyboard, in CSS pixels.
 * 0 when no keyboard is visible. Updates on visualViewport resize/scroll.
 *
 * Browsers expose this via window.visualViewport — height shrinks when the
 * keyboard slides up, leaving the rest of the page visible above it.
 */
export function useKeyboardOffset(): Signal<number> {
  const offset = signal(0);
  useEffect(() => {
    const vv = (window as any).visualViewport as VisualViewport | undefined;
    if (!vv) return;
    const update = () => {
      offset.value = window.innerHeight - vv.height - vv.offsetTop;
    };
    vv.addEventListener("resize", update);
    vv.addEventListener("scroll", update);
    update();
    return () => {
      vv.removeEventListener("resize", update);
      vv.removeEventListener("scroll", update);
    };
  }, []);
  return offset;
}
