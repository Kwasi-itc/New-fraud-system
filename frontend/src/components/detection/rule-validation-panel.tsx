"use client";

import { AlertTriangle, CheckCircle2 } from "lucide-react";

import { Card, CardContent } from "@/components/ui/card";
import type { ValidationResult } from "@/lib/decision-engine-api";

export function RuleValidationPanel({
  validation,
  ruleId,
  isLoading = false,
}: {
  validation?: ValidationResult;
  ruleId?: string;
  isLoading?: boolean;
}) {
  if (isLoading) {
    return (
      <Card className="rounded-xl border border-slate-200 shadow-none">
        <CardContent className="p-4 text-[14px] text-slate-600">
          Validating iteration...
        </CardContent>
      </Card>
    );
  }

  if (!validation) {
    return null;
  }

  const validationRuleResults = validation.rule_results ?? [];
  const validationTriggerErrors = validation.trigger_errors ?? [];
  const validationErrors = validation.errors ?? [];
  const currentRuleResult = ruleId
    ? validationRuleResults.find((result) => result.rule_id === ruleId)
    : null;
  const currentRuleErrors = currentRuleResult?.errors ?? [];
  const hasErrors =
    !validation.valid ||
    validationTriggerErrors.length > 0 ||
    validationErrors.length > 0 ||
    Boolean(currentRuleResult && !currentRuleResult.valid);

  return (
    <Card
      className={
        hasErrors
          ? "rounded-xl border border-amber-200 bg-amber-50 shadow-none"
          : "rounded-xl border border-emerald-200 bg-emerald-50 shadow-none"
      }
    >
      <CardContent className="space-y-3 p-4 text-[14px]">
        <div className="flex items-center gap-2 font-medium">
          {hasErrors ? (
            <AlertTriangle className="size-4 text-amber-700" />
          ) : (
            <CheckCircle2 className="size-4 text-emerald-700" />
          )}
          <span className={hasErrors ? "text-amber-900" : "text-emerald-900"}>
            {hasErrors
              ? "This iteration has validation issues."
              : "This iteration is valid against the current data model."}
          </span>
        </div>

        {validationTriggerErrors.length > 0 ? (
          <div className="space-y-1 text-amber-800">
            {validationTriggerErrors.map((error, index) => (
              <p key={`${error}-${index}`}>Trigger: {error}</p>
            ))}
          </div>
        ) : null}

        {currentRuleResult && !currentRuleResult.valid ? (
          <div className="space-y-1 text-amber-800">
            {currentRuleErrors.map((error, index) => (
              <p key={`${error}-${index}`}>Rule: {error}</p>
            ))}
          </div>
        ) : null}

        {validationErrors.length > 0 ? (
          <div className="space-y-1 text-amber-800">
            {validationErrors.map((error, index) => (
              <p key={`${error}-${index}`}>{error}</p>
            ))}
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}
