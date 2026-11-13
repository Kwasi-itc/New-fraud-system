export type ScreeningListItem = {
  id: string;
  name: string;
  enabled: boolean;
  entity_type: string;
  name_field: string;
  additional_name_fields?: string[];
  threshold?: number;
  forced_outcome?: string;
  outcomes: string[];
};

export type ScenarioScreeningList = {
  rules: ScreeningListItem[];
  can_edit: boolean;
  other_rules: number;
  default_threshold?: number;
};

export const scenarioScreeningKey = (tenant: string, scenario: string) =>
  ["scenario-screening", tenant, scenario] as const;

export async function getScenarioScreeningList(scenario: string): Promise<ScenarioScreeningList> {
  const response = await fetch(`/api/scenario-screening/scenarios/${encodeURIComponent(scenario)}`, { cache: "no-store" });
  if (!response.ok) throw new Error("Screening rules could not be loaded. Retry to see the complete rule list.");
  return response.json();
}

export function screeningDescription(rule: ScreeningListItem, defaultThreshold?: number) {
  const fields = [rule.name_field, ...(rule.additional_name_fields ?? [])].join(" + ");
  const threshold = rule.threshold ?? defaultThreshold;
  return `${rule.entity_type} · ${fields}${threshold === undefined ? "" : ` · ${threshold}% similarity`}`;
}

export function screeningOutcomeLabel(value: string) {
  return ({ approve: "Approve", review: "Review", block_and_review: "Block and review", decline: "Decline" })[value] ?? value;
}
