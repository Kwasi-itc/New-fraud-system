"use client";

import { useEffect } from "react";
import { CheckCircle2, CircleAlert, X } from "lucide-react";

import { cn } from "@/lib/utils";
import { useToastStore } from "@/stores/toast-store";

const TOAST_DURATION_MS = 4500;

export function Toaster() {
  const toasts = useToastStore((state) => state.toasts);
  const dismissToast = useToastStore((state) => state.dismissToast);

  useEffect(() => {
    if (toasts.length === 0) {
      return;
    }

    const timers = toasts.map((toast) =>
      window.setTimeout(() => dismissToast(toast.id), TOAST_DURATION_MS)
    );

    return () => {
      for (const timer of timers) {
        window.clearTimeout(timer);
      }
    };
  }, [dismissToast, toasts]);

  return (
    <div className="pointer-events-none fixed right-4 top-4 z-[200] flex w-[min(92vw,24rem)] flex-col gap-3">
      {toasts.map((toast) => {
        const isError = toast.variant === "error";
        const Icon = isError ? CircleAlert : CheckCircle2;

        return (
          <div
            key={toast.id}
            className={cn(
              "pointer-events-auto rounded-2xl border px-4 py-3 shadow-[0_18px_40px_rgba(15,23,42,0.14)] backdrop-blur",
              isError
                ? "border-red-200 bg-white text-red-950"
                : "border-emerald-200 bg-white text-emerald-950"
            )}
            role="status"
            aria-live="polite"
          >
            <div className="flex items-start gap-3">
              <div
                className={cn(
                  "mt-0.5 rounded-full p-1",
                  isError ? "bg-red-50 text-red-600" : "bg-emerald-50 text-emerald-600"
                )}
              >
                <Icon className="size-4" />
              </div>

              <div className="min-w-0 flex-1">
                <p className="text-sm font-semibold">{toast.title}</p>
                {toast.description ? (
                  <p className="mt-1 text-sm leading-6 text-slate-600">{toast.description}</p>
                ) : null}
              </div>

              <button
                type="button"
                onClick={() => dismissToast(toast.id)}
                className="rounded-lg p-1 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700"
                aria-label="Dismiss notification"
              >
                <X className="size-4" />
              </button>
            </div>
          </div>
        );
      })}
    </div>
  );
}
