# Prompt: Review the decision details design

Act as a senior product designer experienced in fraud investigation and decision-engine tooling. Review the standalone HTML concepts in this directory and provide an independent second opinion. Open the pages in a browser and interact with them before drawing conclusions.

## What to review

- `decision-details-concept-v2.html` is the current decision details concept. Select a rule to open its execution.
- `rule-execution-concept.html` is the current rule execution concept; use its rule selector to explore the examples. It expects a `rule` query parameter; opening it without one displays a not-found message.
- `decision-details-concept.html` is the initial concept, included for historical comparison, not the current recommendation.
- The current application is in `frontend/src/components/detection/decision-detail-page.tsx`; the My Account visual reference is `frontend/src/components/account/account-profile.tsx` and `frontend/src/components/account/account.module.css`.

The HTML uses invented data, thresholds, rules, transaction evidence and workflow details. It is not connected to the application or its APIs. The original request explicitly asked us not to constrain the concept to currently available data. Do not treat any illustrated capability or data field as already implemented.

## User's instructions and reasoning

1. The existing decision page feels out of place, and the presented data is difficult to understand or evaluate. Produce a starting-point HTML concept, following the calm, clean, blue-and-slate theme of My Account.
2. Reduce unnecessary text and make the visual hierarchy clear: the viewer should know what to focus on and what is secondary. The user liked the score visualization and wanted it retained.
3. Remove the page tab bar: this is one page and those tabs are unnecessary. Remove the follow-up pane and the separate decision-information pane. Remove redundant wording, including the review-band badge, the sentence counting contributing rules, and the score calculation/footer disclaimer. The outcome and score visualization remain.
4. Call the list **Rule execution breakdown**. Remove explanatory subtitles under individual rule names. Align names to the left and vertically center the row contents; use larger text.
5. Visually distinguish matched rules that contributed to the score. The highlight should follow the decision outcome: review uses amber and decline uses red. Icons should match the row's color rather than introduce another color. Matched and non-matched text labels remain, so color is not the only signal. The example currently displays a review; alternate outcome colors are styling examples, not separate live decisions.
6. Include aggregate and more complex rules. The user originally requested a separate execution page, then liked the slide-over inspector in the supplied second-opinion prototype. Selecting a rule now opens a sidebar, with an option to open the full page. Both show the rule as constructed, its observed values, logical grouping and match result. Show the configured condition first, followed by its observed value—for example, “Device is new = Yes” followed by “Observed: Yes.” The user disliked the earlier “Met ✓” marker, so the concept uses true/false outcomes instead.
7. Make it easy to navigate directly between rule executions. The dropdown changes the page immediately when a different rule is selected; there is no separate submit button. It was restyled to improve Safari's appearance, but this was not verified in Safari.
8. For aggregate variables, make the filters and time window clear and associate them with the specific variable. Different variables can have different filters, windows, and contributing records; never assume a common scope. The current example intentionally gives the transfer count and transfer sum different scopes.
9. Show contributing transactions only where an aggregate variable is present. Associate evidence with the individual variable, and keep it compact until requested. In this standalone concept, example rows are created only when the corresponding button is clicked; this demonstrates the intended interaction, not a real on-demand API or backend implementation.

## Questions for your review

- Is the information hierarchy clear and the decision outcome easy to evaluate without unnecessary repetition?
- Are matched rules, score contributions, and outcome-based colors understandable and accessible? Does the design avoid confusing a rule match with a confirmed fraud finding?
- Does the execution view accurately convey configured logic, nested AND/OR groups, observed values, and true/false outcomes? What would be confusing for more complex or partially evaluated rules?
- Are per-variable aggregation filters, windows, and evidence sufficiently clear, especially when variables use different scopes? How should missing, incomplete, skipped, errored, or inaccessible evidence be represented?
- Is switching between executions and opening contributing transactions intuitive, keyboard-accessible, and suitable for large datasets?
- What should be changed before implementing this design, including limitations around point-in-time evidence, pagination, permissions, and backend availability?

Give prioritized, specific recommendations and explain trade-offs. Distinguish problems in the concept from implementation questions. Do not assume the prototype's example thresholds, data or behavior exist in production.
