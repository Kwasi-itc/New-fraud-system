// Run inside the frontend container (pipe this file to node --input-type=module).
// Read-only deployment probes: no user login, rule activation, or case creation.
const engine = process.env.DECISION_ENGINE_SERVICE_URL;
const cases = process.env.CASE_MANAGER_SERVICE_URL;
const auth = process.env.AUTH_SERVICE_URL;
if (!engine || !cases || !auth) throw new Error("Workflow runtime URLs are incomplete.");
const id = "00000000-0000-4000-8000-000000000001";
const ownerPath = `/internal/workflow-authoring/tenants/${id}/scenarios/${id}`;
const probes = [
  ["frontend login", "http://127.0.0.1:3000/login", 200],
  ["signed-out workflow access", `http://127.0.0.1:3000/api/workflows/scenarios/${id}`, 401],
  ["authentication schema readiness", `${auth}/readyz`, 204],
  ["decision database readiness", `${engine}/readyz`, 200],
  ["case database readiness", `${cases}/readyz`, 200],
  ["signed identity required", `${engine}${ownerPath}`, 401],
  ["retired credential rejected", `${engine}${ownerPath}`, 401, { Authorization: "Bearer retired-static-token" }],
  ["forged actor headers rejected", `${engine}${ownerPath}`, 401, {
    "X-Workflow-User": id, "X-Workflow-Session": id, "X-Workflow-Version": "1",
  }],
  ["workflow configuration protected", `${engine}/v1/tenants/${id}/scenarios/${id}/workflow-rules`, 401],
];
let failures = 0;
for (const [name, url, expected, headers] of probes) {
  try {
    const response = await fetch(url, { headers, redirect: "manual", signal: AbortSignal.timeout(10000) });
    await response.body?.cancel();
    const ok = response.status === expected;
    console.log(`${ok ? "PASS" : "FAIL"}: ${name} (${response.status}; expected ${expected})`);
    if (!ok) failures++;
  } catch {
    failures++;
    console.log(`FAIL: ${name} (connection unavailable)`);
  }
}
if (failures) process.exitCode = 1;
else console.log("Runtime connectivity and access boundaries verified; signed-in case creation was not exercised.");
