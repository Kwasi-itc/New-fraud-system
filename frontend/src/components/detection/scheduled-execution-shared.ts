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
  if (execution.source === "recurring") {
    return "Recurring schedule";
  }

  return deriveScheduledExecutionItems(execution).length > 0
    ? "One-off explicit items"
    : "One-off ingested candidates";
}

export function formatExecutionRequestBody(value: JSONValue) {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

export function formatRecurringScheduleSummary(schedule: {
  enabled: boolean;
  frequency: string;
  time_of_day: string;
  minute_of_hour: number;
  day_of_week: string;
  day_of_month: number;
  timezone: string;
}) {
  if (!schedule.enabled) {
    return "Disabled";
  }

  if (schedule.frequency === "hourly") {
    return `Hourly at minute ${String(schedule.minute_of_hour ?? 0).padStart(2, "0")} ${schedule.timezone}`;
  }

  if (schedule.frequency === "weekly") {
    const weekday = schedule.day_of_week
      ? `${schedule.day_of_week.slice(0, 1).toUpperCase()}${schedule.day_of_week.slice(1)}`
      : "Monday";
    return `Weekly on ${weekday} at ${schedule.time_of_day} ${schedule.timezone}`;
  }

  if (schedule.frequency === "monthly") {
    return `Monthly on day ${schedule.day_of_month} at ${schedule.time_of_day} ${schedule.timezone}`;
  }

  return `Daily at ${schedule.time_of_day} ${schedule.timezone}`;
}

export function formatNextRun(value: string | null | undefined, timezone = "UTC") {
  if (!value) {
    return null;
  }

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }

  return new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: timezone,
  }).format(date) + ` ${timezone}`;
}
