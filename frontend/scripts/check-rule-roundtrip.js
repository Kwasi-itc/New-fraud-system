const fs = require("fs");
const path = require("path");
const vm = require("vm");
const ts = require("typescript");

function loadRuleBuilder(frontendRoot) {
  const ruleBuilderPath = path.join(frontendRoot, "src", "lib", "rule-builder.ts");
  const source = fs.readFileSync(ruleBuilderPath, "utf8");
  const transpiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2020,
      jsx: ts.JsxEmit.ReactJSX,
    },
    fileName: ruleBuilderPath,
  }).outputText;

  const module = { exports: {} };
  const script = new vm.Script(
    `(function (exports, require, module, __filename, __dirname) { ${transpiled}\n})`,
    { filename: ruleBuilderPath }
  );
  const compiled = script.runInThisContext();
  compiled(
    module.exports,
    (specifier) => {
      throw new Error(`Unexpected runtime import while loading rule-builder: ${specifier}`);
    },
    module,
    ruleBuilderPath,
    path.dirname(ruleBuilderPath)
  );

  return module.exports;
}

function collectFieldRefs(node, payload = [], database = []) {
  if (!node || typeof node !== "object" || Array.isArray(node)) {
    return { payload, database };
  }

  if (node.function === "field_ref") {
    payload.push(node);
  }

  if (node.function === "DatabaseAccess") {
    database.push(node);
  }

  if (node.children) {
    node.children.forEach((child) => collectFieldRefs(child, payload, database));
  }

  if (node.named_children) {
    Object.values(node.named_children).forEach((child) =>
      collectFieldRefs(child, payload, database)
    );
  }

  return { payload, database };
}

function stableJson(value) {
  return JSON.stringify(value, null, 2);
}

function main() {
  const frontendRoot = path.resolve(__dirname, "..");
  const repoRoot = path.resolve(frontendRoot, "..", "..");
  const resultsPath = path.join(repoRoot, "context", "results.json");
  const results = JSON.parse(fs.readFileSync(resultsPath, "utf8"));
  const ruleBuilder = loadRuleBuilder(frontendRoot);

  const sampleRule = results.rules[0];
  const { payload, database } = collectFieldRefs(sampleRule.formula);
  const accessorOptions = ruleBuilder.extractAccessorOptions(payload, database);

  const parsedSimple = ruleBuilder.tryParseAstToConditionGroups(
    sampleRule.formula,
    accessorOptions
  );
  const parsedExpression = ruleBuilder.tryParseAstToExpressionRuleNode(
    sampleRule.formula,
    accessorOptions
  );

  const builderAccessorOptions = ruleBuilder.extractAccessorOptions(
    [
      ruleBuilder.buildFieldRef("amount"),
      ruleBuilder.buildFieldRef("status"),
      ruleBuilder.buildFieldRef("risk_score"),
    ],
    []
  );

  const builderGroups = [
    ruleBuilder.createSimpleRuleGroup({
      openBefore: 1,
      conditions: [
        ruleBuilder.createSimpleRuleCondition({
          left: "payload:field_ref:amount",
          operator: "gt",
          right: "1000",
          rightMode: "constant",
          valueType: "number",
        }),
      ],
    }),
    ruleBuilder.createSimpleRuleGroup({
      openBefore: 1,
      conditions: [
        ruleBuilder.createSimpleRuleCondition({
          left: "payload:field_ref:status",
          operator: "eq",
          right: "pending",
          rightMode: "constant",
          valueType: "string",
        }),
      ],
      closeAfter: 1,
    }),
    ruleBuilder.createSimpleRuleGroup({
      conditions: [
        ruleBuilder.createSimpleRuleCondition({
          left: "payload:field_ref:risk_score",
          operator: "gt",
          right: "",
          rightMode: "constant",
          valueType: "number",
          rightExpression: ruleBuilder.createExpressionOperator({
            operator: "add",
            children: [
              ruleBuilder.createExpressionLeaf({
                mode: "accessor",
                accessorId: "payload:field_ref:amount",
              }),
              ruleBuilder.createExpressionLeaf({
                mode: "constant",
                value: "1000",
                valueType: "number",
              }),
            ],
          }),
        }),
      ],
      closeAfter: 2,
    }),
  ];

  const compiledBuilderAst = ruleBuilder.compileConditionGroupsToAst(
    builderGroups,
    builderAccessorOptions
  );
  const reparsedBuilderGroups = ruleBuilder.tryParseAstToConditionGroups(
    compiledBuilderAst,
    builderAccessorOptions
  );
  const recompiledBuilderAst = reparsedBuilderGroups
    ? ruleBuilder.compileConditionGroupsToAst(reparsedBuilderGroups, builderAccessorOptions)
    : null;

  const deepGroups = [
    ruleBuilder.createSimpleRuleGroup({
      openBefore: 2,
      conditions: [
        ruleBuilder.createSimpleRuleCondition({
          left: "payload:field_ref:amount",
          operator: "gt",
          right: "500",
          rightMode: "constant",
          valueType: "number",
        }),
      ],
    }),
    ruleBuilder.createSimpleRuleGroup({
      openBefore: 1,
      conditions: [
        ruleBuilder.createSimpleRuleCondition({
          left: "payload:field_ref:status",
          operator: "eq",
          right: "review",
          rightMode: "constant",
          valueType: "string",
        }),
      ],
      closeAfter: 1,
    }),
    ruleBuilder.createSimpleRuleGroup({
      conditions: [
        ruleBuilder.createSimpleRuleCondition({
          left: "payload:field_ref:risk_score",
          operator: "gt",
          right: "",
          rightMode: "constant",
          valueType: "number",
          rightExpression: ruleBuilder.createExpressionOperator({
            operator: "add",
            children: [
              ruleBuilder.createExpressionOperator({
                operator: "multiply",
                children: [
                  ruleBuilder.createExpressionLeaf({
                    mode: "accessor",
                    accessorId: "payload:field_ref:amount",
                  }),
                  ruleBuilder.createExpressionLeaf({
                    mode: "constant",
                    value: "2",
                    valueType: "number",
                  }),
                ],
              }),
              ruleBuilder.createExpressionLeaf({
                mode: "constant",
                value: "100",
                valueType: "number",
              }),
            ],
          }),
        }),
      ],
      closeAfter: 2,
    }),
  ];

  const compiledDeepAst = ruleBuilder.compileConditionGroupsToAst(
    deepGroups,
    builderAccessorOptions
  );
  const reparsedDeepGroups = ruleBuilder.tryParseAstToConditionGroups(
    compiledDeepAst,
    builderAccessorOptions
  );
  const recompiledDeepAst = reparsedDeepGroups
    ? ruleBuilder.compileConditionGroupsToAst(reparsedDeepGroups, builderAccessorOptions)
    : null;

  const report = {
    sampleRule: {
      id: sampleRule.id,
      name: sampleRule.name,
      parseableAsSimpleGroups: Boolean(parsedSimple),
      parseableAsExpression: Boolean(parsedExpression),
      summary: ruleBuilder.summarizeRuleFormula(sampleRule.formula),
    },
    builderBracketRule: {
      parseableAfterCompile: Boolean(reparsedBuilderGroups),
      roundTripAstMatches: stableJson(compiledBuilderAst) === stableJson(recompiledBuilderAst),
      compiledAst: compiledBuilderAst,
      reparsedGroups: reparsedBuilderGroups,
    },
    deepBracketRule: {
      parseableAfterCompile: Boolean(reparsedDeepGroups),
      roundTripAstMatches: stableJson(compiledDeepAst) === stableJson(recompiledDeepAst),
      compiledAst: compiledDeepAst,
    },
  };

  console.log(stableJson(report));
}

main();
