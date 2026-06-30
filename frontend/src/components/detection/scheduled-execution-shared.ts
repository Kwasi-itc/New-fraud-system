"use client";

import type { JSONValue, ScheduledExecution } from "@/lib/decision-engine-api";

export function formatExecutionDateTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }

  return new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

export function deriveScheduledExecutionItems(execution: ScheduledExecution) {
  const body = execution.request_body;

  if (Array.isArray(body)) {
    return body;
  }

  if (body && typeof body === "object" && "items" in body) {
    return Array.isArray(body.items) ? body.items : [];
  }

  return [];
}

export function deriveScheduledExecutionCandidateLimit(execution: ScheduledExecution) {
  const body = execution.request_body;

  if (body && typeof body === "object" && "candidate_limit" in body) {
    const candidateLimit = body.candidate_limit;
    return typeof candidateLimit === "number" ? candidateLimit : null;
  }

  return null;
}

export function deriveScheduledExecutionSource(execution: ScheduledExecution) {
  return deriveScheduledExecutionItems(execution).length > 0
    ? "Explicit items"
    : "Ingested candidates";
}

export function formatExecutionRequestBody(value: JSONValue) {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}
