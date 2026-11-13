type Data = Record<string, unknown>;
function object(value: unknown): Data { return value && typeof value === "object" && !Array.isArray(value) ? value as Data : {}; }
function scalar(data: Data, upper: string, lower: string) { const value = data[upper] ?? data[lower]; return typeof value === "string" || typeof value === "number" || typeof value === "boolean" ? value : undefined; }
function strings(value: unknown): string[] { return Array.isArray(value) ? value.filter((v): v is string => typeof v === "string") : []; }

// This projection is applied after case access authorization and before any
// owner data reaches the browser. Do not spread provider/domain objects here.
export function screeningEvidence(value: unknown, expectedID: string) {
  const body = object(value), run = object(body.screening);
  const id = scalar(run, "ID", "id");
  if (id !== expectedID) throw new Error("Invalid screening evidence identity");
  const request = object(run.RequestJSON ?? run.request_json);
  const settings = object(request.provider_config);
  return {
    screening: { id, provider: scalar(run, "Provider", "provider"), status: scalar(run, "Status", "status"), partial: scalar(run, "Partial", "partial"), created_at: scalar(run, "CreatedAt", "created_at"),
      threshold: typeof settings.threshold === "number" ? settings.threshold : undefined,
      datasets: strings(settings.datasets),
      queries: Array.isArray(request.queries) ? request.queries.map(value => { const q = object(value); return Object.fromEntries(["name", "type", "birth_date", "nationality", "country", "registration_number", "address"].filter(key => typeof q[key] === "string").map(key => [key, q[key]])); }) : [],
    },
    matches: Array.isArray(body.matches) ? body.matches.map(value => {
      const match = object(value), payload = object(match.Payload ?? match.payload), properties = object(payload.properties);
      return { id: scalar(match, "ID", "id"), entity_id: scalar(match, "EntityID", "entity_id"), name: scalar(match, "Name", "name"), status: scalar(match, "Status", "status"), score: scalar(match, "Score", "score"),
        schema: typeof payload.schema === "string" ? payload.schema : undefined, datasets: strings(payload.datasets),
        properties: Object.fromEntries(["alias", "country", "nationality", "birthDate", "address", "topics"].map(key => [key, strings(properties[key])]).filter(([, values]) => values.length)),
      };
    }) : [],
  };
}
