import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { buildScenarioDefinitions, CLIENT_SCOPES, findMissingRules, omittedRules, parseArgs, portableModel, validateDefinitionsAgainstModel } from './provision-fraud-scenarios-ec2.mjs';

const tenantId = 'd7943f99-4402-428a-a539-0b2ecb6fcc17';
const clientId = '552e8400-e29b-41d4-a716-446655440000';

test('requires a gateway origin and explicit tenant UUID', () => {
  assert.throws(() => parseArgs([]), /--host/);
  assert.throws(() => parseArgs(['--host', '10.0.0.2:8080']), /--tenant-id/);
  assert.throws(() => parseArgs(['--host', '10.0.0.2:8080', '--tenant-id', 'root']), /must be a UUID/);
});

test('parses host, tenant, model path, and late callback threshold', () => {
  const options = parseArgs([
    '--host', '10.0.0.2:8088',
    '--tenant-id', tenantId,
    '--client-id', clientId,
    '--model-file', './model.json',
    '--late-callback-seconds', '450',
    '--structuring-limit', '1200',
    '--structuring-band-percent', '8',
  ]);
  assert.equal(options.origin, 'http://10.0.0.2:8088');
  assert.equal(options.tenantId, tenantId);
  assert.equal(options.clientId, clientId);
  assert.equal(options.tokenUrl, 'http://10.0.0.2:8088/oauth/token');
  assert.deepEqual(CLIENT_SCOPES, ['models:read', 'scenarios:read', 'scenarios:write']);
  assert.equal(options.lateCallbackSeconds, 450);
  assert.equal(options.structuringLimit, 1200);
  assert.equal(options.nearLimitPercent, 8);
  assert.equal(options.modelFile.endsWith('/model.json'), true);
});

test('rejects unsafe/non-origin hosts and invalid thresholds', () => {
  assert.throws(() => parseArgs(['--host', 'http://user:pass@10.0.0.2:8080', '--tenant-id', tenantId]), /origin/);
  assert.throws(() => parseArgs(['--host', '10.0.0.2:8080/path', '--tenant-id', tenantId]), /origin/);
  assert.throws(() => parseArgs(['--host', '10.0.0.2:8080', '--tenant-id', tenantId, '--client-id', clientId, '--late-callback-seconds', '0']), /positive integer/);
  assert.throws(() => parseArgs(['--host', '10.0.0.2:8080', '--tenant-id', tenantId, '--client-id', 'not-a-uuid']), /registered API client UUID/);
  assert.throws(() => parseArgs(['--host', '10.0.0.2:8080', '--tenant-id', tenantId, '--structuring-band-percent', '101']), /no more than 100/);
});

test('defines five scenarios with aggregate rules and configurable callback threshold', () => {
  const scenarios = buildScenarioDefinitions(450);
  assert.equal(scenarios.length, 5);
  assert.equal(scenarios[0].rules.length, 4);
  assert.equal(scenarios[1].rules.length, 6);
  assert.equal(scenarios[2].rules.length, 6);
  assert.equal(scenarios[3].rules.length, 6);
  assert.equal(scenarios[4].rules.length, 6);
  assert.equal(scenarios[4].rules[0].formula.children[1].constant, 450);
  assert.equal(scenarios[0].thresholds.join(','), '50,80,95');
  const configured = buildScenarioDefinitions(300, { structuringLimit: 1200, nearLimitPercent: 8 });
  const configuredRule = configured[3].rules.find(({ stable_rule_id }) => stable_rule_id === 'structuring-near-limit');
  assert.equal(configuredRule.description.includes('1104 and 1200'), true);
});

test('uses durations supported by the Decision Engine aggregate evaluator', () => {
  const durations = new Set();
  const visit = (node) => {
    if (!node || typeof node !== 'object') return;
    if (node.function === 'TimeAdd') durations.add(node.named_children.duration.constant);
    for (const child of node.children ?? []) visit(child);
    for (const child of Object.values(node.named_children ?? {})) visit(child);
  };
  for (const scenario of buildScenarioDefinitions()) {
    visit(scenario.trigger);
    for (const rule of scenario.rules) visit(rule.formula);
  }
  assert.deepEqual([...durations].sort(), ['24h', '60m', 'P30D']);
});

test('all published formula fields and related paths exist in the supplied model', async () => {
  const envelope = JSON.parse(await readFile(new URL('../fraud-data-model.json', import.meta.url), 'utf8'));
  const model = portableModel(envelope.data_model);
  assert.doesNotThrow(() => validateDefinitionsAgainstModel(buildScenarioDefinitions(), model));
});

test('only rules outside current Aggregator scope remain omitted', () => {
  assert.deepEqual(omittedRules.map(({ id }) => id), ['A5', 'D2']);
});

test('detects missing supported rules and refuses conflicting or duplicate stable IDs', () => {
  const [present, missing] = buildScenarioDefinitions()[0].rules;
  assert.deepEqual(findMissingRules([present], [present, missing]), [missing]);
  assert.throws(() => findMissingRules([{ ...present, score_modifier: 99 }], [present]), /conflicts/);
  assert.throws(() => findMissingRules([present, present], [present]), /duplicate stable_rule_id/);
});

test('supports explicit dry run and help without requiring target arguments', () => {
  assert.equal(parseArgs(['--dry-run']).dryRun, true);
  assert.equal(parseArgs(['--dry-run', '--host', '10.0.0.2:8088', '--tenant-id', tenantId]).dryRun, true);
  assert.equal(parseArgs(['--help']).help, true);
  assert.throws(() => parseArgs(['--dry-run', '--late-callback-seconds', '0']), /positive integer/);
});

test('requires a registered client ID for live provisioning', () => {
  assert.throws(() => parseArgs(['--host', '10.0.0.2:8088', '--tenant-id', tenantId]), /--client-id/);
});
