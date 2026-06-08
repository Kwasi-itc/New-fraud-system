"use client";

import { useQuery } from "@tanstack/react-query";
import type { CSSProperties } from "react";
import { getHealthcheck } from "@/lib/api";
import { useUiStore } from "@/stores/ui-store";

const panelStyle: CSSProperties = {
  maxWidth: 960,
  margin: "48px auto",
  padding: 32,
  border: "1px solid var(--border)",
  borderRadius: 24,
  background: "var(--surface)",
  backdropFilter: "blur(10px)",
  boxShadow: "0 24px 60px rgba(52, 45, 31, 0.08)",
};

const badgeStyle: CSSProperties = {
  display: "inline-flex",
  alignItems: "center",
  gap: 8,
  padding: "8px 12px",
  borderRadius: 999,
  background: "rgba(13, 148, 136, 0.1)",
  color: "var(--accent-strong)",
  fontSize: 13,
  fontWeight: 700,
  letterSpacing: "0.04em",
  textTransform: "uppercase",
};

export default function HomePage() {
  const count = useUiStore((state) => state.count);
  const increment = useUiStore((state) => state.increment);
  const reset = useUiStore((state) => state.reset);

  const healthcheckQuery = useQuery({
    queryKey: ["healthcheck"],
    queryFn: getHealthcheck,
  });

  return (
    <main style={{ padding: "0 20px 48px" }}>
      <section style={panelStyle}>
        <span style={badgeStyle}>Next.js + Zustand + TanStack Query</span>
        <h1 style={{ fontSize: "clamp(2.4rem, 6vw, 4.8rem)", marginBottom: 12 }}>
          Marble frontend scaffold
        </h1>
        <p style={{ color: "var(--muted)", fontSize: 18, lineHeight: 1.6, maxWidth: 700 }}>
          This app is wired with App Router, a shared TanStack Query client, and a small Zustand
          store. Replace the sample healthcheck query with your backend endpoints as the API
          contracts settle.
        </p>

        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit, minmax(260px, 1fr))",
            gap: 20,
            marginTop: 32,
          }}
        >
          <article
            style={{
              padding: 24,
              borderRadius: 20,
              border: "1px solid var(--border)",
              background: "rgba(255, 255, 255, 0.65)",
            }}
          >
            <h2 style={{ marginTop: 0 }}>Zustand store</h2>
            <p style={{ color: "var(--muted)" }}>
              Local UI state lives in a dedicated store under <code>stores/</code>.
            </p>
            <p style={{ fontSize: 40, margin: "12px 0" }}>{count}</p>
            <div style={{ display: "flex", gap: 12 }}>
              <button onClick={increment} style={buttonStyle}>
                Increment
              </button>
              <button onClick={reset} style={secondaryButtonStyle}>
                Reset
              </button>
            </div>
          </article>

          <article
            style={{
              padding: 24,
              borderRadius: 20,
              border: "1px solid var(--border)",
              background: "rgba(255, 255, 255, 0.65)",
            }}
          >
            <h2 style={{ marginTop: 0 }}>TanStack Query</h2>
            <p style={{ color: "var(--muted)" }}>
              The sample query hits <code>/api/health</code> through the shared query client.
            </p>
            <pre
              style={{
                margin: 0,
                padding: 16,
                borderRadius: 16,
                overflowX: "auto",
                background: "#1e1d1a",
                color: "#f4f1ea",
                fontSize: 14,
              }}
            >
              {JSON.stringify(
                {
                  status: healthcheckQuery.status,
                  data: healthcheckQuery.data,
                  error:
                    healthcheckQuery.error instanceof Error
                      ? healthcheckQuery.error.message
                      : null,
                },
                null,
                2,
              )}
            </pre>
          </article>
        </div>
      </section>
    </main>
  );
}

const buttonStyle: CSSProperties = {
  border: 0,
  borderRadius: 999,
  padding: "12px 18px",
  background: "var(--accent)",
  color: "white",
  cursor: "pointer",
  fontWeight: 700,
};

const secondaryButtonStyle: CSSProperties = {
  ...buttonStyle,
  background: "#d6d0c3",
  color: "var(--foreground)",
};
