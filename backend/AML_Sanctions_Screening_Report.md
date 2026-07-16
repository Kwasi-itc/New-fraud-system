# AML/Sanctions Screening Compliance Report

## 1. System Overview & Identification
*   **1.1 System Name & Ownership:** The screening tool is a proprietary, in-house built solution named the **IT Consortium Fraud Detection System**.
*   **1.2 Vendor Details:** As a proprietary system, there is no third-party vendor. The system is designed, developed, and maintained entirely internally by IT Consortium.
*   **1.3 Processing Architecture:** The system employs a hybrid processing architecture, utilizing both real-time and batch processing capabilities to ensure comprehensive risk detection without disrupting payment flows.
*   **1.4 Modular Service Architecture:** The screening capability operates within a modular service-based architecture that separates ingestion, decisioning, screening, and case management functions. This separation strengthens control, traceability, and operational resilience.

## 2. Screening Scope & Coverage
*   **2.1 Transaction Screening Focus:** The core function of the system is comprehensive transaction screening, designed to monitor and evaluate financial flows for both sanctions compliance and fraud detection.
*   **2.2 Transaction Coverage:** The system monitors 100% of both inbound and outbound transactions, providing universal coverage across all payment channels operated by the institution.
*   **2.3 Customer & Entity Linkage (Dual-Methodology):** The system utilizes a dual-layered screening approach during transaction processing:
    *   **Watchlist Screening:** Sender and receiver details are extracted and screened against designated sanctions and watchlists in real-time.
    *   **Behavioral & Rule-Based Monitoring:** Transactions are simultaneously evaluated against customized, pre-defined scenarios and rules designed to detect anomalous transactional behavior and complex fraud patterns.
*   **2.4 Manual / Investigator-Led Screening Capability:** In addition to automated transaction screening, the system supports manual or freeform screening searches. This enables investigators and compliance officers to conduct ad hoc name, entity, and counterparty screening outside the normal automated transaction flow where enhanced due diligence is required.
*   **2.5 Continuous Screening Capability:** The platform also supports continuous screening of monitored persons, entities, and counterparties, allowing the institution to re-screen previously onboarded or previously reviewed records whenever the underlying provider datasets are updated.

## 3. Data Sources & Watchlist Management
*   **3.1 Data Acquisition Method:** To ensure high data integrity, the **IT Consortium Fraud Detection System** pulls watchlist data directly from the official issuing authorities via direct CSV file downloads, rather than relying on a third-party intermediary.
*   **3.2 Configured Watchlists:** The system currently maintains and screens transactions against the following designated watchlists:
    *   UN Consolidated Sanctions List
    *   OFAC (US Treasury) Lists
    *   EU and UK (OFSI) Sanctions Lists
    *   Ghana Local Designations / FIC Lists
    *   PEP (Politically Exposed Persons) Databases
    *   **Internal Blacklist:** A proprietary, dynamically updated database containing accounts, individuals, and entities previously flagged, blocked, or investigated for fraudulent or suspicious activity.
*   **3.3 List Update Frequency & Synchronisation:**
    *   **Scheduled Updates:** By default, the system is configured to perform automated weekly updates of the external regulatory lists.
    *   **On-Demand Synchronisation:** System administrators or authorized users can manually trigger an immediate update/synchronisation of any or all watchlists at any time, depending on operational requirements or fast-moving global sanctions updates.
*   **3.4 Dataset Freshness Monitoring:** The system includes controls for checking the freshness and availability of screening datasets, enabling operations teams to verify that watchlist content is current and synchronized before relying on the screening results.
*   **3.5 Delta Update & Re-Screening Controls:** Where a list or screening dataset changes, the platform supports update-job processing and re-screening of affected monitored entities, thereby reducing the risk that previously cleared entities remain unscreened after list changes.

## 4. Operational Methodology & Workflow
*   **4.1 Comprehensive Matching Logic:** The IT Consortium Fraud Detection System utilizes a highly configurable, multi-parameter matching engine to evaluate text, names, and transaction data. The system supports the following matching operators to ensure both precision and broad coverage:
    *   **Fuzzy Matching:** Utilizes similarity algorithms (`Fuzzy Match`, `Fuzzy Match Any Of`) to detect spelling variations, phonetic similarities, and typographical errors against single or multiple watchlist records.
    *   **Deterministic / Exact Matching:** Employs precise operators including Exact Equal (`eq / =`), Not Equal (`neq / !=`), `Contains`, `In` (list matching), `Starts with`, and `Ends with`.
    *   **Data Validation & Normalization:** Includes string manipulation (`lower`, `upper`) and data presence checks (`Is null`, `Is Empty`, `Is Not Empty`) to clean and standardize data prior to evaluation.
*   **4.2 Alert Generation & Risk Scoring:** 
    *   Rather than relying on binary pass/fail triggers, the system evaluates each transaction and assigns it a cumulative risk score based on watchlist hits and behavioral rules.
    *   The "processor owner" (the operating institution) has the autonomy to configure the exact score thresholds that determine whether a transaction is automatically allowed to pass or is placed on a temporary hold.
    *   Any transaction exceeding the designated risk threshold is immediately flagged and routed to a centralized Transactions Monitor for investigation.
*   **4.3 Escalation & Disposition Workflow:** The system enforces a strict, multi-tiered review process for all flagged transactions:
    *   **Level 1 Investigation (Fraud Analyst):** A designated security expert/Fraud Analyst reviews the flagged transaction within the monitor. They conduct an initial investigation to dismiss false positives or validate true risks.
    *   **Case Building & Escalation:** If the analyst determines the alert is a valid threat or true watchlist match, they compile the evidence into a case file within the system and escalate it to the AMLRO.
    *   **Level 2 Final Evaluation (AMLRO):** The Anti-Money Laundering Reporting Officer (AMLRO) conducts the final review of the escalated case. The AMLRO maintains ultimate authority on the disposition of the transaction and is responsible for filing a Suspicious Transaction Report (STR) with the Financial Intelligence Centre (FIC) if required.
*   **4.4 Match Review & Analyst Commentary:** The system supports structured match review, analyst comments, and decision logging on individual screening matches. This ensures that false positives, true matches, and review rationale are documented at the case and alert level.
*   **4.5 Whitelisting / Suppression Controls:** The platform includes a dedicated screening whitelist capability, enabling authorized reviewers to suppress repeatedly reviewed benign matches where justified, while maintaining an auditable record of that decision.
*   **4.6 Retry & Exception Handling:** Failed screening requests, provider exceptions, and dataset update failures can be retried through controlled workflows, reducing operational disruption during temporary upstream outages or connectivity failures.
*   **4.7 Supporting Evidence Management:** The system allows investigators to attach and retrieve supporting files and evidence related to screening reviews, strengthening case documentation and regulatory defensibility.

## 5. Governance, Reporting, & Evidence
*   **5.1 System Integration & Architecture:** 
    *   The **IT Consortium Fraud Detection System** is integrated into the institutionâ€™s core processing environment via secure **REST APIs**. 
    *   This API-driven architecture allows the system to seamlessly ingest and evaluate both real-time and batch transaction data from all payment gateways and channels, returning risk scores and hold/pass directives with minimal latency.
*   **5.2 Audit Trail & Record Keeping (Evidentiary Standards):** 
    *   To satisfy regulatory scrutiny and internal audit requirements, the system maintains an immutable, permanent log of all screening activities. 
    *   This comprehensive audit trail captures:
        *   The raw transaction data screened.
        *   The specific rules triggered and the resulting risk score.
        *   The exact version/timestamp of the watchlist against which the match occurred.
        *   All manual interventions, including Level 1 Fraud Analyst investigation notes, rationale for dismissing false positives, and Level 2 AMLRO disposition records.
*   **5.3 Operational Metrics & Reporting Dashboards:** 
    *   The system features a dedicated operational dashboard that provides the compliance team and executive management with real-time visibility into screening efficacy.
    *   Key metrics tracked include total transaction screening volumes, alert generation rates, false-positive ratios, true watchlist hits, and the total number of escalations resulting in Suspicious Transaction Reports (STRs) filed with the FIC. 
*   **5.4 Request Traceability & Monitoring:** 
    *   Each screening transaction and related system interaction is traceable through request identifiers, structured service logs, health checks, and operational monitoring endpoints.
    *   This strengthens incident investigation, operational oversight, and internal audit review.
*   **5.5 Provider Status & Screening Lifecycle Tracking:** 
    *   The system records the lifecycle of each screening request, including pending, in-progress, completed, failed, and review states.
    *   This enables the institution to demonstrate end-to-end control over screening execution, exception handling, and final disposition.
*   **5.6 Case Management Integration:** 
    *   The screening process is integrated with the institution's case management workflow, allowing confirmed alerts, review outcomes, supporting evidence, and escalation actions to feed directly into formal case records for investigation and AML governance.
