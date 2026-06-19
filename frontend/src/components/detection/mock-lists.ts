export type DetectionList = {
  id: string;
  name: string;
  description: string;
  type: string;
  count: string;
  values: string[];
};

export const initialDetectionLists: DetectionList[] = [
  {
    id: "high-risk-mcc",
    name: "High_risk_MCC",
    description: "List of MCC codes with high risk profile",
    type: "Generic text",
    count: "2 values",
    values: ["7995", "4829"],
  },
  {
    id: "welcome-to-marble",
    name: "Welcome to Marble",
    description: "Need a whitelist or blacklist ? The list is your friend :)",
    type: "Generic text",
    count: "3 values",
    values: ["alpha", "beta", "gamma"],
  },
  {
    id: "blacklist-card-tokens",
    name: "blacklist_Card_tokens",
    description: "Blacklisted card tokens",
    type: "Generic text",
    count: "3 values",
    values: ["card_tok_001", "card_tok_002", "card_tok_003"],
  },
  {
    id: "blacklist-users",
    name: "blacklist_users",
    description: "Blacklisted users",
    type: "Generic text",
    values: [
      "e345cdd3-eed9-4362-99b7-d63accebe112",
      "8a852da6-52ae-409e-a83c-5a3a7aa3de62",
      "c8edc6b7-cc99-4842-b832-620188258209",
      "4f611faa-2e06-4e99-8e73-4771738a8cb5",
      "b52e29d8-1b07-48dd-8aff-2b38f767febf",
      "0c97fcd8-0758-46f6-8a55-bd6bc9517898",
    ],
    count: "6 values",
  },
  {
    id: "whitelist-companies",
    name: "whitelist_companies",
    description: "Whitelist of non targeted companies",
    type: "Generic text",
    count: "5 values",
    values: ["Acme Ltd", "Nova Pay", "Delta Works", "Blue Labs", "North Star"],
  },
  {
    id: "ip-blacklist",
    name: "IP Blacklist",
    description: "Blacklisted users",
    type: "IP addresses and subnets",
    count: "0 values",
    values: [],
  },
];

export function formatListCount(values: string[]) {
  return `${values.length} value${values.length === 1 ? "" : "s"}`;
}

export function getDetectionList(listId: string) {
  return (
    initialDetectionLists.find((item) => item.id === listId) ??
    initialDetectionLists[0]
  );
}
