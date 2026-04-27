import { useSignal } from "@preact/signals";

export interface ComposerProps {
  disabled: boolean;
  onSend: (text: string) => void;
  placeholder?: string;
}

export function Composer({ disabled, onSend, placeholder }: ComposerProps) {
  const text = useSignal("");
  const send = () => {
    if (!text.value) return;
    onSend(text.value);
    text.value = "";
  };
  return (
    <div class="composer">
      <textarea
        value={text.value}
        onInput={e => { text.value = (e.currentTarget as HTMLTextAreaElement).value; }}
        placeholder={placeholder ?? "Type and Send..."}
        disabled={disabled}
        rows={1}
      />
      <button class="send-btn" disabled={disabled || !text.value} onClick={send}>Send</button>
    </div>
  );
}
