"use client";

import { useCallback, useSyncExternalStore } from "react";

const onboardingCompletedPrefix = "fraud-manager:onboarding-completed:v1";
const navigationTourStartedPrefix = "fraud-manager:navigation-tour-started:v1";
const navigationTourCompletedPrefix = "fraud-manager:navigation-tour-completed:v1";
const onboardingStateEvent = "fraud-manager-onboarding-state-changed";

function storageKey(prefix: string, tenantId: string) {
  return `${prefix}:${tenantId || "unknown-tenant"}`;
}

function readFlag(prefix: string, tenantId: string) {
  if (typeof window === "undefined" || !tenantId) {
    return false;
  }

  return window.localStorage.getItem(storageKey(prefix, tenantId)) === "true";
}

function writeFlag(prefix: string, tenantId: string) {
  if (typeof window === "undefined" || !tenantId) {
    return;
  }

  window.localStorage.setItem(storageKey(prefix, tenantId), "true");
  window.dispatchEvent(new Event(onboardingStateEvent));
}

function subscribeToOnboardingState(onStoreChange: () => void) {
  window.addEventListener("storage", onStoreChange);
  window.addEventListener(onboardingStateEvent, onStoreChange);

  return () => {
    window.removeEventListener("storage", onStoreChange);
    window.removeEventListener(onboardingStateEvent, onStoreChange);
  };
}

function getServerSnapshot() {
  return false;
}

export function useFraudManagerOnboardingCompleted(tenantId: string) {
  const getSnapshot = useCallback(
    () => readFlag(onboardingCompletedPrefix, tenantId),
    [tenantId]
  );

  return useSyncExternalStore(subscribeToOnboardingState, getSnapshot, getServerSnapshot);
}

export function markFraudManagerOnboardingCompleted(tenantId: string) {
  writeFlag(onboardingCompletedPrefix, tenantId);
}

export function hasFraudManagerNavigationTourStarted(tenantId: string) {
  return readFlag(navigationTourStartedPrefix, tenantId);
}

export function markFraudManagerNavigationTourStarted(tenantId: string) {
  writeFlag(navigationTourStartedPrefix, tenantId);
}

export function markFraudManagerNavigationTourCompleted(tenantId: string) {
  writeFlag(navigationTourStartedPrefix, tenantId);
  writeFlag(navigationTourCompletedPrefix, tenantId);
}

export function hasFraudManagerNavigationTourCompleted(tenantId: string) {
  return readFlag(navigationTourCompletedPrefix, tenantId);
}
