const state = {
  fixtures: [],
  selectedFixture: null,
  session: null,
  edits: {},
  selectedStageKey: "tenant",
  running: false,
};

const els = {
  fixtureList: document.querySelector("#fixtureList"),
  fixtureSearch: document.querySelector("#fixtureSearch"),
  fixtureCategory: document.querySelector("#fixtureCategory"),
  fixtureTitle: document.querySelector("#fixtureTitle"),
  fixtureDescription: document.querySelector("#fixtureDescription"),
  createSessionBtn: document.querySelector("#createSessionBtn"),
  previousStageBtn: document.querySelector("#previousStageBtn"),
  runCurrentBtn: document.querySelector("#runCurrentBtn"),
  nextStageBtn: document.querySelector("#nextStageBtn"),
  runAllBtn: document.querySelector("#runAllBtn"),
  resetBtn: document.querySelector("#resetBtn"),
  stageFlow: document.querySelector("#stageFlow"),
  stageTitle: document.querySelector("#stageTitle"),
  stageDescription: document.querySelector("#stageDescription"),
  stageStatus: document.querySelector("#stageStatus"),
  stageContent: document.querySelector("#stageContent"),
  serviceTimeline: document.querySelector("#serviceTimeline"),
  materializedJson: document.querySelector("#materializedJson"),
};

init();

async function init() {
  bindEvents();
  await loadHealth();
  await loadFixtures();
  render();
}

function bindEvents() {
  els.fixtureSearch.addEventListener("input", renderFixtures);
  els.createSessionBtn.addEventListener("click", createSession);
  els.previousStageBtn.addEventListener("click", previousStage);
  els.runCurrentBtn.addEventListener("click", runCurrentStage);
  els.nextStageBtn.addEventListener("click", nextStage);
  els.runAllBtn.addEventListener("click", runAll);
  els.resetBtn.addEventListener("click", resetSession);
}

async function loadHealth() {
  try {
    await apiGet("/api/health");
  } catch (error) {
    console.warn(error.message);
  }
}

async function loadFixtures() {
  const data = await apiGet("/api/fixtures");
  state.fixtures = data.fixtures;
}

function render() {
  renderFixtures();
  renderTop();
  renderFlow();
  renderStage();
  renderTrace();
}

function renderFixtures() {
  const query = els.fixtureSearch.value.trim().toLowerCase();
  const filtered = state.fixtures
    .filter((fixture) => `${fixture.name} ${fixture.category} ${fixture.description} ${fixture.complexity_label}`.toLowerCase().includes(query))
    .sort((a, b) => (a.demo_order ?? 999) - (b.demo_order ?? 999));
  els.fixtureList.innerHTML = "";
  filtered.forEach((fixture, index) => {
    const btn = document.createElement("button");
    btn.className = `fixture-card ${state.selectedFixture?.id === fixture.id ? "active" : ""}`;
    btn.innerHTML = `<strong>${index + 1}. ${escapeHtml(fixture.name)}</strong><span class="muted">${escapeHtml(fixture.description)}</span><span class="fixture-meta"><span class="pill">${escapeHtml(fixture.complexity_label || "demo")}</span><span class="pill">${fixture.rule_count} rules</span>${fixture.invalid_expected ? '<span class="pill">invalid case</span>' : ""}</span>`;
    btn.addEventListener("click", () => selectFixture(fixture.id));
    els.fixtureList.appendChild(btn);
  });
}

async function selectFixture(fixtureId) {
  const data = await apiGet(`/api/fixtures/${encodeURIComponent(fixtureId)}`);
  state.selectedFixture = data.fixture;
  state.session = null;
  state.selectedStageKey = "tenant";
  state.edits = defaultEdits(data.fixture.editable);
  render();
}

function defaultEdits(editable) {
  return {
    input_fields: structuredClone(editable.input_fields || {}),
    thresholds: structuredClone(editable.thresholds || {}),
    rule_scores: structuredClone(editable.rule_scores || {}),
    generated_counts: structuredClone(editable.generated_counts || []),
    expected: structuredClone(editable.expected || {}),
    expected_rules: structuredClone(editable.expected_rules || {}),
    rule_edits: structuredClone(editable.rule_edits || { constants: {}, operators: {} }),
    stage_config: structuredClone(editable.stage_config || {}),
  };
}

function renderTop() {
  if (!state.selectedFixture) {
    els.fixtureCategory.textContent = "No fixture selected";
    els.fixtureTitle.textContent = "Choose a fixture to start";
    els.fixtureDescription.textContent = "Create each system part, retrieve it, edit rules, then evaluate.";
  } else {
    els.fixtureCategory.textContent = state.selectedFixture.category.replaceAll("_", " ");
    els.fixtureTitle.textContent = state.selectedFixture.name;
    els.fixtureDescription.textContent = state.selectedFixture.description;
  }
  const hasPending = !!state.session?.stages?.some((stage) => stage.status === "pending");
  const stageIndex = currentStageIndex();
  const stageCount = activeStages().length;
  els.createSessionBtn.disabled = state.running || !state.selectedFixture;
  els.createSessionBtn.textContent = state.session ? "Create New Session" : "Create Session";
  els.previousStageBtn.disabled = state.running || !state.selectedFixture || stageIndex <= 0;
  els.runCurrentBtn.disabled = state.running || !state.session || currentStage()?.status !== "pending";
  els.nextStageBtn.disabled = state.running || !state.selectedFixture || stageIndex >= stageCount - 1;
  els.runAllBtn.disabled = state.running || !state.session || !hasPending;
  els.resetBtn.disabled = state.running || !state.session;
}

function renderFlow() {
  const stages = activeStages();
  els.stageFlow.innerHTML = stages.map((stage, index) => `
    <button class="flow-node ${stage.status} ${stage.key === state.selectedStageKey ? "active" : ""}" data-stage="${stage.key}">
      <span class="flow-index">${index + 1}</span>
      <span>${escapeHtml(stage.label)}</span>
    </button>
  `).join("");
  els.stageFlow.querySelectorAll("[data-stage]").forEach((btn) => {
    btn.addEventListener("click", () => {
      state.selectedStageKey = btn.dataset.stage;
      render();
    });
  });
}

function defaultStages() {
  return [
    ["tenant", "Tenant"],
    ["tables", "Tables"],
    ["fields", "Fields"],
    ["records", "Records"],
    ["scenario", "Scenario"],
    ["rules", "Rules"],
    ["validation", "Validation"],
    ["publish", "Publish"],
    ["evaluation", "Evaluation"],
    ["history", "History"],
  ].map(([key, label]) => ({ key, label, description: "", status: "pending", summary: "" }));
}

function currentStage() {
  return activeStages().find((stage) => stage.key === state.selectedStageKey);
}

function activeStages() {
  return state.session?.stages || defaultStages();
}

function currentStageIndex() {
  return activeStages().findIndex((stage) => stage.key === state.selectedStageKey);
}

function renderStage() {
  const stage = currentStage() || defaultStages().find((item) => item.key === state.selectedStageKey);
  els.stageTitle.textContent = stage?.label || "Stage";
  els.stageDescription.textContent = stage?.summary || stage?.description || "Create a session to run this stage.";
  els.stageStatus.className = `status ${stage?.status || "pending"}`;
  els.stageStatus.textContent = stage?.status || "pending";
  if (!state.selectedFixture) {
    els.stageContent.className = "stage-content empty";
    els.stageContent.textContent = "Select a fixture from the sidebar.";
    return;
  }
  els.stageContent.className = "stage-content";
  els.stageContent.innerHTML = `
    <div class="stage-two-pane">
      <section class="stage-pane">
        <div class="pane-title"><h4>Inputs</h4><span>Editable values and fixed context</span></div>
        ${renderStageInputs(state.selectedStageKey)}
      </section>
      <section class="stage-pane">
        <div class="pane-title"><h4>Results</h4><span>Create and retrieve evidence</span></div>
        ${renderStageResults(state.selectedStageKey, stage)}
      </section>
    </div>
  `;
  els.stageContent.querySelectorAll("[data-edit], [data-stage-config]").forEach((input) => {
    input.addEventListener("change", handleEditChange);
    input.addEventListener("input", handleEditChange);
  });
  els.stageContent.querySelectorAll("[data-rule-edit]").forEach((input) => {
    input.addEventListener("change", handleRuleEditChange);
    input.addEventListener("input", handleRuleEditChange);
  });
}

function renderStageInputs(stageKey) {
  if (!state.selectedFixture) return "";
  if (stageKey === "tenant") {
    const cfg = state.edits.stage_config?.tenant || {};
    return formSection("Tenant", [
      stageConfigInput("tenant", "name_prefix", "Tenant name prefix", cfg.name_prefix || "demo tenant"),
      stageConfigInput("tenant", "external_key_prefix", "External key prefix", cfg.external_key_prefix || "demo_ext"),
    ]);
  }
  if (stageKey === "tables") {
    const cfg = state.edits.stage_config?.tables || {};
    return formSection("Transaction table", [
      stageConfigInput("tables", "transactions_name_prefix", "Name prefix", cfg.transactions_name_prefix || "transactions"),
      stageConfigInput("tables", "transactions_alias", "Alias", cfg.transactions_alias || "Transactions"),
      stageConfigInput("tables", "transactions_description", "Description", cfg.transactions_description || "Demo transactions table"),
    ]) + formSection("Account table", [
      stageConfigInput("tables", "accounts_name_prefix", "Name prefix", cfg.accounts_name_prefix || "accounts"),
      stageConfigInput("tables", "accounts_alias", "Alias", cfg.accounts_alias || "Accounts"),
      stageConfigInput("tables", "accounts_description", "Description", cfg.accounts_description || "Demo accounts table"),
    ]);
  }
  if (stageKey === "fields") {
    return renderFieldDefinitionInputs();
  }
  if (stageKey === "records") {
    return renderInputFields(state.edits.input_fields) + (state.edits.generated_counts.length ? renderGeneratedCounts(state.edits.generated_counts) : "") + renderSeedPreview();
  }
  if (stageKey === "scenario") {
    return formSection("Scenario", [
      disabledInput("Scenario name", state.selectedFixture.name),
      disabledInput("Trigger object type", state.selectedFixture.raw.object_type || "$transactions"),
    ]) + renderNumberMap("Thresholds", "thresholds", state.edits.thresholds);
  }
  if (stageKey === "rules") {
    return renderRuleInputs();
  }
  if (stageKey === "validation") {
    return renderExpected(state.edits.expected, {});
  }
  if (stageKey === "publish") {
    return formSection("Publication", [disabledInput("Action", "publish"), disabledInput("Iteration", state.session?.stages?.find((item) => item.key === "scenario")?.created?.iteration?.id || "created in Scenario stage")]);
  }
  if (stageKey === "evaluation") {
    return renderInputFields(state.edits.input_fields) + renderExpected(state.edits.expected, state.edits.expected_rules);
  }
  if (stageKey === "history") {
    const decisionId = state.session?.result?.evaluation?.decision?.id || "created in Evaluation stage";
    return formSection("Decision lookup", [disabledInput("Decision id", decisionId)]);
  }
  return "";
}

function renderStageResults(stageKey, stage) {
  if (!stage || stage.status === "pending") return `<div class="empty">Run this stage to see readable results.</div>`;
  if (stage.status === "failed") return `<div class="result-error">${escapeHtml(stage.error)}</div>`;
  if (stage.status === "skipped") return resultCards([{ label: "Skipped", value: stage.summary || "No action required" }]);
  if (stageKey === "tenant") {
    const tenant = stage.retrieved?.tenant || stage.created?.tenant || {};
    return resultCards([
      { label: "Tenant ID", value: tenant.id },
      { label: "Name", value: tenant.name },
      { label: "Provisioned", value: stage.created?.provisioned ? "yes" : "unknown" },
      { label: "Retrieved", value: tenant.id ? "yes" : "no", status: tenant.id ? "passed" : "failed" },
    ]);
  }
  if (stageKey === "tables") {
    const tables = stage.retrieved?.tables || [];
    return resultCards([
      { label: "Created", value: `${tables.length || 2} tables` },
      { label: "Transaction ID", value: stage.created?.transactions?.id },
      { label: "Account ID", value: stage.created?.accounts?.id },
    ]) + tableView(["Alias", "Name", "ID"], tables.map((table) => [table.alias, table.name, table.id]));
  }
  if (stageKey === "fields") {
    const txnFields = stage.retrieved?.transaction_fields || [];
    return resultCards([
      { label: "Transaction fields", value: txnFields.length },
      { label: "Account link", value: stage.created?.account_link?.name || "account" },
      { label: "Data model tables", value: (stage.retrieved?.data_model?.tables || []).join(", ") },
    ]) + tableView(["Field", "Type", "Nullable"], txnFields.map((field) => [field.name, field.data_type, String(field.nullable)]));
  }
  if (stageKey === "records") {
    const samples = stage.retrieved?.samples || [];
    return resultCards([
      { label: "Generated", value: stage.created?.generated_records || 0 },
      { label: "Explicit records", value: stage.created?.records || 0 },
      { label: "Input ingested", value: stage.created?.input_record_ingested ? "yes" : "no" },
    ]) + tableView(["Object ID", "Amount", "Status", "Owner"], samples.map((row) => [row.object_id, row.amount, row.status, row.owner_id]));
  }
  if (stageKey === "scenario") {
    return resultCards([
      { label: "Scenario ID", value: stage.created?.scenario?.id },
      { label: "Iteration ID", value: stage.created?.iteration?.id },
      { label: "Retrieved scenarios", value: (stage.retrieved?.scenarios || []).length },
    ]);
  }
  if (stageKey === "rules") {
    const rules = stage.retrieved?.rules || stage.created?.rules || [];
    return resultCards([{ label: "Created rules", value: rules.length }]) + tableView(["Rule", "Score", "ID"], rules.map((rule) => [rule.name, rule.score_modifier, rule.id]));
  }
  if (stageKey === "validation") {
    const comparison = stage.retrieved?.comparison || {};
    return resultCards([
      { label: "Actual valid", value: stage.created?.validation?.valid, status: stage.created?.validation?.valid ? "passed" : "failed" },
      { label: "Matches expected", value: comparison.matches ? "yes" : "no", status: comparison.matches ? "passed" : "failed" },
      { label: "Expected valid", value: comparison.expected ?? "not specified" },
    ]);
  }
  if (stageKey === "publish") {
    return resultCards([
      { label: "Publication", value: stage.status === "passed" ? "published" : stage.status },
      { label: "Known publications", value: (stage.retrieved?.publications || []).length || "available" },
    ]);
  }
  if (stageKey === "evaluation") return renderEvaluationResults(stage);
  if (stageKey === "history") {
    return resultCards([
      { label: "Persisted", value: stage.retrieved?.in_listing ? "yes" : "unknown", status: stage.retrieved?.in_listing ? "passed" : "pending" },
      { label: "History count", value: stage.retrieved?.listing_count || 0 },
      { label: "Decision ID", value: stage.definition?.decision_id },
    ]);
  }
  return resultCards([{ label: "Summary", value: stage.summary }]);
}

function renderRuleInputs() {
  const source = state.session?.materialized?.rule_diagram_model || state.selectedFixture.rule_diagram_model || [];
  const legacySource = state.session?.materialized?.rule_edit_model || state.selectedFixture.rule_edit_model || [];
  if (!source.length && legacySource.length) return renderLegacyRuleInputs(legacySource);
  if (!source.length) return `<div class="empty">No rules available for this fixture.</div>`;
  return source.map((rule) => `
    <article class="rule-flow">
      <div class="rule-flow-head">
        <div>
          <strong>${escapeHtml(rule.rule)}</strong>
          <p>${escapeHtml(rule.formula)}</p>
        </div>
        <span class="pill">score ${escapeHtml(String(rule.score_modifier))}</span>
      </div>
      <div class="condition-list">
        ${(rule.conditions || []).map((condition) => renderRuleCondition(condition)).join("") || '<div class="empty">No editable conditions.</div>'}
      </div>
    </article>
  `).join("");
}

function renderLegacyRuleInputs(source) {
  return source.map((rule) => `
    <article class="rule-flow">
      <div class="rule-flow-head">
        <div>
          <strong>${escapeHtml(rule.rule)}</strong>
          <p>${escapeHtml(rule.formula)}</p>
        </div>
        <span class="pill">score ${escapeHtml(String(rule.score_modifier))}</span>
      </div>
      <div class="legacy-node-list">
        ${(rule.nodes || []).filter((node) => node.editable_operator || node.editable_constant).map((node) => renderLegacyAstNode(node)).join("") || '<div class="empty">No editable rule inputs.</div>'}
      </div>
    </article>
  `).join("");
}

function renderLegacyAstNode(node) {
  const path = escapeHtml(node.path);
  if (node.editable_operator) {
    const value = state.edits.rule_edits.operators[node.path] ?? node.value;
    return `
      <div class="condition-card compact">
        <div class="condition-main">
          <span>Operator</span>
          <strong>${escapeHtml(node.human || node.label || "Rule condition")}</strong>
        </div>
        <div class="condition-controls">
          <div class="field"><label>Operator</label><select data-rule-edit="operator" data-path="${path}">
            ${node.editable_operator.map((option) => `<option value="${escapeHtml(option)}" ${option === value ? "selected" : ""}>${escapeHtml(option)}</option>`).join("")}
          </select></div>
        </div>
      </div>
    `;
  }
  const value = state.edits.rule_edits.constants[node.path] ?? node.value;
  return `
    <div class="condition-card compact">
      <div class="condition-main">
        <span>Value</span>
        <strong>${escapeHtml(node.human || "Constant")}</strong>
      </div>
      <div class="condition-controls">
        <div class="field"><label>Value</label><input data-rule-edit="constant" data-path="${path}" value="${escapeHtml(Array.isArray(value) ? value.join(", ") : String(value ?? ""))}" /></div>
      </div>
    </div>
  `;
}

function renderRuleCondition(condition) {
  const operator = condition.operator_edit;
  const constants = condition.constant_edits || [];
  const expression = [condition.left || condition.formula || "", condition.right ? condition.operator_symbol || condition.operator : "", condition.right || ""].filter(Boolean).join(" ");
  return `
    <div class="condition-card">
      ${condition.joiner ? `<span class="joiner">${escapeHtml(condition.joiner)}</span>` : ""}
      <div class="condition-main">
        <span>${escapeHtml(condition.title || "Condition")}</span>
        <strong>${escapeHtml(expression)}</strong>
      </div>
      <div class="condition-controls">
        ${operator ? ruleOperatorSelect(operator) : disabledInput("Operator", condition.operator_symbol || condition.operator || "value")}
        ${constants.map((constant) => ruleConstantInput(constant)).join("")}
      </div>
    </div>
  `;
}

function ruleOperatorSelect(operator) {
  const path = escapeHtml(operator.path);
  const value = state.edits.rule_edits.operators[operator.path] ?? operator.value;
  return `<div class="field"><label>Operator</label><select data-rule-edit="operator" data-path="${path}">${operator.options.map((option) => `<option value="${escapeHtml(option)}" ${option === value ? "selected" : ""}>${escapeHtml(option)}</option>`).join("")}</select></div>`;
}

function ruleConstantInput(constant) {
  const value = state.edits.rule_edits.constants[constant.path] ?? constant.value;
  return `<div class="field"><label>${escapeHtml(constant.label || "Value")}</label><input data-rule-edit="constant" data-path="${escapeHtml(constant.path)}" value="${escapeHtml(Array.isArray(value) ? value.join(", ") : String(value ?? ""))}" /></div>`;
}

function renderEvaluationResults(stage) {
  const comparison = stage?.retrieved?.comparison;
  if (!comparison) {
    return `<div class="empty">Run evaluation to see decision results.</div>`;
  }
  const rows = comparison.rules.map((rule) => `<tr><td>${escapeHtml(rule.name)}</td><td>${escapeHtml(String(rule.expected))}</td><td>${escapeHtml(String(rule.actual))}</td><td><span class="status ${rule.matches ? "passed" : "failed"}">${rule.matches ? "match" : "diff"}</span></td></tr>`).join("");
  return `
    <p>${escapeHtml(comparison.summary)}</p>
    <div class="metric-grid">
      <div class="metric"><span>Trigger</span><strong>${escapeHtml(String(comparison.triggered.actual))}</strong></div>
      <div class="metric"><span>Score</span><strong>${escapeHtml(String(comparison.score.actual))}</strong></div>
      <div class="metric"><span>Outcome</span><strong>${escapeHtml(String(comparison.outcome.actual || "none"))}</strong></div>
    </div>
    <table class="diff-table"><thead><tr><th>Rule</th><th>Expected</th><th>Actual</th><th>Status</th></tr></thead><tbody>${rows}</tbody></table>
  `;
}

function renderTrace() {
  const timeline = state.session?.timeline || [];
  if (!timeline.length) {
    els.serviceTimeline.className = "trace empty";
    els.serviceTimeline.textContent = "No service calls yet.";
  } else {
    els.serviceTimeline.className = "trace";
    els.serviceTimeline.innerHTML = timeline.map((call) => `<div class="trace-row"><strong>${escapeHtml(call.service)}</strong><span>${escapeHtml(call.method)}</span><span class="trace-path">${escapeHtml(call.path)}</span><span class="status ${call.status < 400 ? "passed" : "failed"}">${call.status}</span></div>`).join("");
  }
  els.materializedJson.textContent = JSON.stringify(state.session?.materialized || state.selectedFixture?.raw || {}, null, 2);
}

function renderInputFields(fields) {
  const rows = Object.entries(fields).map(([key, value]) => fieldInput("input_fields", key, value, typeof value === "number" ? "number" : "text")).join("");
  return `<div class="form-section"><h4>Input transaction</h4><div class="field-grid">${rows || "No editable fields"}</div></div>`;
}

function renderFieldDefinitionInputs() {
  const fields = ["amount:int", "status:string enum", "account_id:string", "ip:ip_address", "merchant:string", "email:string", "country:string", "owner_id:string", "event_time:timestamp", "note:string"];
  return formSection("Transaction fields", fields.map((field) => disabledInput("Field", field)).join(""));
}

function renderSeedPreview() {
  const summary = state.selectedFixture?.seed_summary || {};
  return formSection("Seed data", [
    disabledInput("Seed decisions", summary.seed_decisions || 0),
    disabledInput("Explicit records", summary.records || 0),
    disabledInput("Generated batches", (summary.generated_batches || []).length),
  ]);
}

function renderNumberMap(title, section, values) {
  const rows = Object.entries(values).map(([key, value]) => fieldInput(section, key, value, "number")).join("");
  return `<div class="form-section"><h4>${title}</h4><div class="field-grid">${rows || "No editable values"}</div></div>`;
}

function renderGeneratedCounts(counts) {
  const rows = counts.map((count, index) => fieldInput("generated_counts", String(index), count, "number")).join("");
  return `<div class="form-section"><h4>Generated batches</h4><div class="field-grid">${rows}</div></div>`;
}

function renderExpected(expected, expectedRules) {
  const expectedRows = Object.entries(expected).map(([key, value]) => {
    if (typeof value === "boolean") return boolSelect("expected", key, value);
    if (typeof value === "number") return fieldInput("expected", key, value, "number");
    return fieldInput("expected", key, value ?? "");
  }).join("");
  const ruleRows = Object.entries(expectedRules).map(([key, value]) => outcomeSelect("expected_rules", key, value)).join("");
  return `<div class="form-section"><h4>Expected result</h4><div class="field-grid">${expectedRows || "No expected summary"}</div></div><div class="form-section"><h4>Expected rule outcomes</h4><div class="field-grid">${ruleRows || "No expected rule outcomes"}</div></div>`;
}

function fieldInput(section, key, value, type = "text") {
  return `<div class="field"><label>${escapeHtml(labelize(key))}</label><input data-edit="${section}" data-key="${escapeHtml(key)}" type="${type}" value="${escapeHtml(String(value ?? ""))}" /></div>`;
}

function stageConfigInput(stage, key, label, value) {
  return `<div class="field"><label>${escapeHtml(label)}</label><input data-stage-config="${stage}" data-key="${escapeHtml(key)}" value="${escapeHtml(String(value ?? ""))}" /></div>`;
}

function disabledInput(label, value) {
  return `<div class="field"><label>${escapeHtml(label)}</label><input class="readonly-input" value="${escapeHtml(String(value ?? ""))}" disabled /></div>`;
}

function formSection(title, controls) {
  const body = Array.isArray(controls) ? controls.join("") : controls;
  return `<div class="form-section"><h4>${escapeHtml(title)}</h4><div class="field-grid">${body}</div></div>`;
}

function resultCards(items) {
  return `<div class="result-card-grid">${items.map((item) => `<div class="result-card"><span>${escapeHtml(item.label)}</span><strong>${escapeHtml(String(item.value ?? "not available"))}</strong>${item.status ? `<em class="status ${item.status}">${escapeHtml(item.status)}</em>` : ""}</div>`).join("")}</div>`;
}

function tableView(headers, rows) {
  if (!rows.length) return `<div class="empty">No rows returned yet.</div>`;
  return `<table class="diff-table"><thead><tr>${headers.map((item) => `<th>${escapeHtml(item)}</th>`).join("")}</tr></thead><tbody>${rows.map((row) => `<tr>${row.map((cell) => `<td>${escapeHtml(String(cell ?? ""))}</td>`).join("")}</tr>`).join("")}</tbody></table>`;
}

function boolSelect(section, key, value) {
  return `<div class="field"><label>${escapeHtml(labelize(key))}</label><select data-edit="${section}" data-key="${escapeHtml(key)}"><option value="true" ${value === true ? "selected" : ""}>true</option><option value="false" ${value === false ? "selected" : ""}>false</option></select></div>`;
}

function outcomeSelect(section, key, value) {
  return `<div class="field"><label>${escapeHtml(key)}</label><select data-edit="${section}" data-key="${escapeHtml(key)}">${["hit", "no_hit", "error"].map((item) => `<option value="${item}" ${item === value ? "selected" : ""}>${item}</option>`).join("")}</select></div>`;
}

function handleEditChange(event) {
  const input = event.target;
  if (input.dataset.stageConfig) {
    const stage = input.dataset.stageConfig;
    state.edits.stage_config[stage] = state.edits.stage_config[stage] || {};
    state.edits.stage_config[stage][input.dataset.key] = input.value;
    if (state.session) state.session = null;
    renderTop();
    return;
  }
  const section = input.dataset.edit;
  const key = input.dataset.key;
  let value = parseInputValue(input);
  if (section === "generated_counts") state.edits.generated_counts[Number(key)] = Number(value);
  else state.edits[section][key] = value;
  if (state.session) state.session = null;
  renderTop();
}

function handleRuleEditChange(event) {
  const input = event.target;
  const path = input.dataset.path;
  if (input.dataset.ruleEdit === "operator") {
    state.edits.rule_edits.operators[path] = input.value;
  } else {
    state.edits.rule_edits.constants[path] = input.value;
  }
  if (state.session) state.session = null;
  renderTop();
  renderTrace();
}

function parseInputValue(input) {
  let value = input.value;
  if (input.type === "number") value = Number(value);
  else if (value === "true") value = true;
  else if (value === "false") value = false;
  else if (value === "null") value = null;
  return value;
}

async function createSession() {
  if (!state.selectedFixture) return;
  await withRunning(async () => {
    const data = await apiPost("/api/sessions", { fixture_id: state.selectedFixture.id, edits: state.edits });
    state.session = data.session;
    state.selectedStageKey = "tenant";
  });
}

async function runCurrentStage() {
  if (!state.session) return;
  await runStage(state.selectedStageKey);
}

function previousStage() {
  moveStage(-1);
}

function nextStage() {
  moveStage(1);
}

function moveStage(delta) {
  const stages = activeStages();
  const index = currentStageIndex();
  const next = stages[index + delta];
  if (!next) return;
  state.selectedStageKey = next.key;
  render();
}

async function runAll() {
  if (!state.session) return;
  await withRunning(async () => {
    const data = await apiPost(`/api/sessions/${state.session.id}/run-all`, {});
    state.session = data.session;
  });
}

async function resetSession() {
  if (!state.session) return;
  await withRunning(async () => {
    const data = await apiPost(`/api/sessions/${state.session.id}/reset`, {});
    state.session = data.session;
    state.selectedStageKey = "tenant";
  });
}

async function runStage(stageKey) {
  await withRunning(async () => {
    const data = await apiPost(`/api/sessions/${state.session.id}/stages/${stageKey}/run`, {});
    state.session = data.session;
    state.selectedStageKey = stageKey;
  });
}

async function withRunning(fn) {
  state.running = true;
  renderTop();
  try {
    await fn();
  } catch (error) {
    alert(error.message);
  } finally {
    state.running = false;
    render();
  }
}

async function apiGet(path) {
  const response = await fetch(path);
  return handleResponse(response);
}

async function apiPost(path, payload) {
  const response = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload) });
  return handleResponse(response);
}

async function handleResponse(response) {
  const data = await response.json();
  if (!response.ok) {
    const detail = data.detail ? `: ${typeof data.detail === "string" ? data.detail : JSON.stringify(data.detail)}` : "";
    throw new Error(`${data.error || "Request failed"}${detail}`);
  }
  return data;
}

function labelize(value) {
  return value.replaceAll("_", " ");
}

function escapeHtml(value) {
  return String(value).replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&#039;");
}
