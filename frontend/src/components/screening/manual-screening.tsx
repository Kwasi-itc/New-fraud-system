"use client";

import { createUuid } from "@/lib/create-uuid";

import { Fragment, useEffect, useRef, useState, type FormEvent } from "react";
import {
  Search,
  ShieldCheck,
  ShieldAlert,
  LoaderCircle,
  User,
  Building2,
  Calendar,
  Globe,
  MapPin,
  Fingerprint,
  FileText,
  SlidersHorizontal,
  CheckCircle2,
  AlertTriangle,
  Tag,
  RotateCcw,
  History,
  AlertCircle,
  Users,
  ChevronDown,
  ChevronUp,
  Briefcase,
  Layers,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { OpenInvestigation } from "./open-investigation";
import { SourceMonitor } from "./source-monitor";
import { IdentifierFields, type IdentifierInput } from "./identifier-fields";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { clsx } from "clsx";

type DatasetRef = { scope: "global" | "tenant"; id: string };
type Capabilities = {
  id: string;
  name: string;
  configured: boolean;
  health: string;
  fields: Record<string, string[]>;
  dataset_selection: boolean;
  limitations: string[];
  entity_types: string[];
  threshold_min: number;
  threshold_max: number;
  limit_max: number;
  providers?: Capabilities[];
  profiles?: string[];
  profile_fields?: Record<string, Record<string, string[]>>;
  datasets?: { ref: DatasetRef; name: string; generation?: string | null; category: string }[];
};

type SearchInput = {
  identifiers?: IdentifierInput[];
  operation_id: string;
  name: string;
  entity_type: string;
  threshold: number;
  limit: number;
  birth_date?: string;
  nationality?: string;
  country?: string;
  registration_number?: string;
  address?: string;
  datasets?: string[];
  provider?: string;
  profile?: string;
  dataset_selection?: DatasetRef[];
};

type Candidate = {
  id: string;
  entity_id: string;
  name: string;
  score: number;
  schema: string;
  datasets: string[] | null;
  properties: Record<string, string[]>;
  enriched?: boolean;
};

type Result = {
  id: string;
  status: string;
  partial?: boolean;
  matches?: Candidate[];
  query?: SearchInput;
  created_at?: string;
};

const pending = (status: string) => ["pending", "queued", "processing"].includes(status);

const labels: Record<string, string> = {
  alias: "Also known as",
  country: "Countries",
  nationality: "Nationality",
  birthDate: "Date of birth",
  birth_date: "Date of birth",
  address: "Address",
  topics: "Risk topics & roles",
  source: "Source reference",
  sourceRecord: "Source record",
  registrationNumber: "Registration number",
  registration_number: "Registration number",
  datasetVersion: "Dataset version",
  matchingProfile: "Matching profile",
  matchedAlias: "Matched name",
  matchReason: "Why it matched",
  matchingStrategy: "Matching method",
  matchedField: "Matched field",
  comparisonValues: "Compared names",
  nameScore: "Name score",
  attributeEvidence: "Identity details",
  sourceIdentifiers: "Source identifiers",
  relationship: "Relationship to PEP",
  relatedPep: "Connected PEP",
  familyMembers: "Known immediate family",
  associates: "Known close associates",
};

const datasetDisplayNames: Record<string, string> = {
  "ofac-sdn": "OFAC SDN",
  "un-consolidated": "UN Sanctions List",
  uksl: "UK Sanctions List",
  "eu-fsf": "EU Sanctions",
  "worldbank-debarred": "World Bank Debarred",
  "gh-pep-executive": "Ghana Executive PEPs",
  "gh-pep-parliament": "Ghana Parliament PEPs",
  "gh-pep-soe": "Ghana SOE & Judiciary PEPs",
  "gh-pep-rca": "Ghana PEP Relatives & Associates",
  "gh-pep-local-gov": "Ghana MMDCEs & Regional Ministers",
  "gh-pep-parties": "Ghana Political Party Executives",
  "gh-pep-judiciary": "Ghana Judicial Service Supreme Court PEPs",
  us_ofac_sdn: "OFAC SDN",
  us_ofac_cons: "OFAC Consolidated",
  un_sc_sanctions: "UN Sanctions List",
  gb_hmt_sanctions: "UK Sanctions",
  eu_fsf: "EU Sanctions",
  interpol_red_notices: "Interpol Red Notices",
  pep: "Global PEP",
  every_politician: "EveryPolitician PEP",
  worldbank_debarred: "World Bank Debarred",
  eu_cor_members: "EU Committee of the Regions",
  us_bis_denied: "US BIS Denied Persons",
  ch_seco_sanctions: "Swiss SECO Sanctions",
  au_dfat_sanctions: "Australia Sanctions",
  ca_sema_sanctions: "Canada Sanctions",
};

function formatDatasetName(id: string): string {
  if (datasetDisplayNames[id]) return datasetDisplayNames[id];
  return id
    .replace(/^us_/, "US ")
    .replace(/^gb_/, "UK ")
    .replace(/^eu_/, "EU ")
    .replace(/^un_/, "UN ")
    .replace(/[_-]+/g, " ")
    .replace(/\b[a-z]/g, c => c.toUpperCase());
}

const roleAndTopicDisplayNames: Record<string, string> = {
  "role.pep": "Politically Exposed Person (PEP)",
  "pep": "Politically Exposed Person (PEP)",
  "role.rca": "Relative or Close Associate (RCA)",
  "rca": "Relative or Close Associate (RCA)",
  "sanction": "Sanctioned Entity",
  "sanction.linked": "Linked to Sanctions",
  "debarment": "Debarred Entity",
  "crime": "Financial Crime / Wanted",
  "crime.fin": "Financial Crime",
  "crime.theft": "Fraud & Theft",
  "crime.war": "War Crimes",
  "corp.disqual": "Disqualified Director",
  "export.control": "Export Control",
  "poi": "Person of Interest",
};

function formatPropertyValue(key: string, value: string): string {
  if (key === "topics" || key === "role") {
    return roleAndTopicDisplayNames[value.toLowerCase()] ?? value;
  }
  return value;
}

const field = "mt-1.5 w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 transition-colors focus:border-blue-600 focus:outline-none focus:ring-2 focus:ring-blue-100 disabled:bg-slate-50 disabled:text-slate-500";

const identityKeys = new Set(["alias", "relationship", "relatedPep", "familyMembers", "associates", "birthDate", "birth_date", "nationality", "country", "address", "registrationNumber", "registration_number", "sourceIdentifiers"]);
const evidenceKeys = new Set(["matchingProfile", "matchedAlias", "matchReason", "matchingStrategy", "matchedField", "comparisonValues", "nameScore", "attributeEvidence"]);
const provenanceKeys = new Set(["topics", "source", "sourceRecord", "datasetVersion"]);
const quickFactKeys = new Set(["birthDate", "birth_date", "nationality", "country", "registrationNumber", "registration_number", "address"]);

function candidatePropertyGroups(properties: Candidate["properties"]) {
  const entries = Object.entries(properties).filter(([, values]) => values?.length);
  const assigned = new Set<string>();
  const groups = [
    { title: "Identity information", keys: identityKeys },
    { title: "Match evidence", keys: evidenceKeys },
    { title: "Source provenance", keys: provenanceKeys },
  ].map(group => ({
    title: group.title,
    entries: entries.filter(([key]) => {
      if (!group.keys.has(key)) return false;
      assigned.add(key);
      return true;
    }),
  })).filter(group => group.entries.length);

  const other = entries.filter(([key]) => !assigned.has(key));
  if (other.length) groups.push({ title: "Other details", entries: other });
  return groups;
}

function parseCandidateContext(match: Candidate) {
  const props = match.properties;
  const programs = props.programs || [];
  const statusRaw = props.sourceStatus?.[0] || "";
  const topics = (props.topics || []).map(t => t.toLowerCase());

  // Detect category
  const isPep = topics.some(t => t.includes("pep")) || programs.some(p => p.toLowerCase().includes("pep")) || match.datasets?.some(d => d.includes("pep") && !d.includes("rca"));
  const isRca = topics.some(t => t.includes("rca")) || programs.some(p => p.toLowerCase().includes("rca")) || match.datasets?.some(d => d.includes("rca")) || !!props.relatedPep?.[0] || !!props.relationship?.[0];
  const isSanctioned = topics.some(t => t.includes("sanction")) || programs.some(p => p.toLowerCase().includes("sanction") || p.toLowerCase().includes("sdgt") || p.toLowerCase().includes("sdnt") || p.toLowerCase().includes("iran"));
  const isDebarred = topics.some(t => t.includes("debar")) || match.datasets?.some(d => d.includes("worldbank") || d.includes("debarred"));

  // Status (active vs cooling-off)
  let statusBadge = "Active";
  if (statusRaw.includes("cooling_off") || programs.some(p => p.toLowerCase().includes("cooling-off"))) {
    statusBadge = "Cooling-Off";
  }

  // Position is distinct from a PEP classification. Prefer the explicit source
  // role; older snapshots may retain the specific position only in programs.
  const genericRoleLabel = /\b(?:pep|politically exposed|office holder|cooling[-\s]?off)\b/i;
  const sourceRole = (props.sourceStatus ?? [])
    .map(status => status.match(/(?:^|;)\s*Role:\s*([^;]+)/i)?.[1].trim())
    .find(value => value && !genericRoleLabel.test(value));
  let role = sourceRole ?? "";
  if (!role && isPep) {
    role = programs.find(value => value.trim() && !genericRoleLabel.test(value)) ?? "";
  }
  if (!role && isDebarred) {
    role = props.sanctionsImposed?.[0] || "Ineligible / Debarred Firm";
  }

  // Extract Connected PEP & Relationship for RCA
  let connectedPep = props.relatedPep?.[0] || "";
  let relationship = props.relationship?.[0] || "";
  if (!connectedPep && statusRaw) {
    const pepMatch = statusRaw.match(/Connected PEP:\s*([^;]+)/i);
    if (pepMatch) connectedPep = pepMatch[1].trim();
  }
  if (!connectedPep && programs) {
    const pepProg = programs.find(p => p.startsWith("Connected PEP:"));
    if (pepProg) connectedPep = pepProg.replace(/^Connected PEP:\s*/i, "").trim();
  }
  if (!relationship && statusRaw) {
    const relMatch = statusRaw.match(/Relationship:\s*([^;]+)/i);
    if (relMatch) relationship = relMatch[1].trim();
  }
  if (!relationship && programs) {
    const relProg = programs.find(p => p.startsWith("Relationship:"));
    if (relProg) relationship = relProg.replace(/^Relationship:\s*/i, "").trim();
  }

  // Known Family & Associates
  let family = props.familyMembers || [];
  if (family.length === 0 && statusRaw) {
    const famMatch = statusRaw.match(/Known Family:\s*([^;]+)/i);
    if (famMatch) family = famMatch[1].split(/,\s*/);
  }
  let associates = props.associates || [];
  if (associates.length === 0 && statusRaw) {
    const assocMatch = statusRaw.match(/Known Associates:\s*([^;]+)/i);
    if (assocMatch) associates = assocMatch[1].split(/,\s*/);
  }

  // Compliance note / sanctions imposed
  const complianceNotice = props.sanctionsImposed?.[0] || "";
  const aliases = props.alias || [];

  return { isPep, isRca, isSanctioned, isDebarred, statusBadge, role, connectedPep, relationship, family, associates, complianceNotice, aliases };
}

async function request<T>(url: string, init: RequestInit): Promise<T> {
  const response = await fetch(url, { ...init, cache: "no-store" });
  const body = await response.json();
  if (!response.ok) throw new Error(body.error ?? "Search is unavailable. Please try again.");
  return body;
}

export function ManualScreening({ scope }: { scope: string }) {
  const searchParams = useSearchParams();
  const initialId = searchParams.get("id");

  const catalog = useQuery({
    queryKey: ["screening-capabilities", scope],
    queryFn: () => request<Capabilities>("/api/screening-search/capabilities", {}),
    retry: false,
    gcTime: 0,
  });

  const [name, setName] = useState("");
  const [identifiers, setIdentifiers] = useState<IdentifierInput[]>([]);
  const [provider, setProvider] = useState("internal-watchlists");
  const [profile, setProfile] = useState("name-fuzzy-v1");
  const [selectedDatasets, setSelectedDatasets] = useState<DatasetRef[]>([]);
  const selectionScope = useRef<string | null>(null);
  const [expandedMatches, setExpandedMatches] = useState<Set<string>>(new Set());
  const toggleExpand = (id: string) => {
    setExpandedMatches(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };
  const [entityType, setEntityType] = useState("Person");
  const [threshold, setThreshold] = useState(70);
  const [limit, setLimit] = useState(10);
  const [birthDate, setBirthDate] = useState("");
  const [country, setCountry] = useState("");
  const [nationality, setNationality] = useState("");
  const [registration, setRegistration] = useState("");
  const [address, setAddress] = useState("");
  const [datasets, setDatasets] = useState("");
  const [historical, setHistorical] = useState(false);
  const [refining, setRefining] = useState(false);
  const nameInput = useRef<HTMLInputElement>(null);
  const datePickerRef = useRef<HTMLInputElement>(null);
  const [submitted, setSubmitted] = useState<SearchInput>();
  const [result, setResult] = useState<Result>();
  const [error, setError] = useState("");
  const [sending, setSending] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const submission = useRef(false);
  const busy = sending || (!!result && pending(result.status) && !error);

  // Load from searchParams if navigated from history page
  useEffect(() => {
    if (initialId && !result) {
      setHistorical(true);
      setResult({ id: initialId, status: "pending" });
    }
  }, [initialId, result]);

  // Initialize from this workspace's actual catalog once. Refreshes must not
  // reselect lists the investigator deliberately cleared.
  useEffect(() => {
    const datasets = catalog.data?.providers?.find(p => p.id === "internal-watchlists")?.datasets;
    if (!datasets || selectionScope.current === scope) return;
    selectionScope.current = scope;
    setSelectedDatasets(datasets.filter(dataset => !!dataset.generation).map(dataset => dataset.ref));
  }, [catalog.data, scope]);

  useEffect(() => {
    if (!result || !pending(result.status) || error) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    let attempts = 0;
    async function poll() {
      try {
        const next = await request<Result>(`/api/screening-search/${result!.id}`, { signal: controller.signal });
        if (controller.signal.aborted) return;
        if (next.query) setSubmitted(next.query);
        if (!pending(next.status)) { setResult(next); return; }
        if (++attempts >= 60) { setError("This search is taking longer than expected. Check its status again shortly."); return; }
        timer = setTimeout(poll, 2000);
      } catch (cause) {
        if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : "Unable to load results.");
      }
    }
    void poll();
    return () => { controller.abort(); clearTimeout(timer); };
  }, [result, error, refresh]);

  async function submit(input: SearchInput) {
    if (submission.current) return;
    submission.current = true;
    setSending(true);
    setHistorical(false);
    setError("");
    setResult(undefined);
    setSubmitted(input);
    try {
      const created = await request<Result>("/api/screening-search", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(input),
      });
      setResult({ id: created.id, status: "pending" });
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Unable to start search.");
    } finally {
      submission.current = false;
      setSending(false);
    }
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!catalogReady || incompatibleDraft || !settingsSupported) return;
    if (!name.trim()) { setError("Enter a name to search."); return; }
    setRefining(false);
    void submit({
      operation_id: createUuid(),
      name: name.trim(),
      entity_type: entityType,
      threshold,
      limit,
      ...Object.fromEntries(Object.entries(identityValues).filter(([key, value]) => supports(key) && value.trim())),
      ...(datasetSelection && datasets.trim() ? { datasets: datasets.split(",").map(value => value.trim()).filter(Boolean) } : {}),
      ...(internal ? { provider, profile, dataset_selection: selectedDatasets } : {}),
      ...(supports("identifiers") && identifiers.length ? { identifiers } : {}),
    });
  }

  function refine() {
    const query = result?.query;
    if (!query || busy) return;
    setName(query.name);
    setEntityType(query.entity_type);
    setProvider(query.provider ?? "opensanctions");
    selectionScope.current = scope;
    setSelectedDatasets(query.dataset_selection ?? []);
    setProfile(query.profile ?? "exact-name-v1");
    setIdentifiers(query.identifiers ?? []);
    setThreshold(query.threshold);
    setLimit(query.limit);
    setBirthDate(query.birth_date ?? "");
    setCountry(query.country ?? "");
    setNationality(query.nationality ?? "");
    setRegistration(query.registration_number ?? "");
    setAddress(query.address ?? "");
    setDatasets(query.datasets?.join(", ") ?? "");
    setRefining(true);
    nameInput.current?.focus();
  }

  const providers = catalog.data?.providers ?? (catalog.data ? [catalog.data] : []);
  const cap = catalog.data?.providers ? providers.find(value => value.id === provider) : catalog.data;
  const internal = provider === "internal-watchlists";
  const catalogReady = !!cap?.configured && !catalog.isError &&
    Array.isArray(cap.entity_types) && !!cap.fields &&
    Number.isInteger(cap.threshold_min) && Number.isInteger(cap.threshold_max) &&
    cap.threshold_min >= 0 && cap.threshold_max <= 100 && cap.threshold_min <= cap.threshold_max &&
    Number.isInteger(cap.limit_max) && cap.limit_max >= 1 && cap.limit_max <= 50;

  const entityTypes = (cap?.entity_types ?? []).filter(value =>
    (internal ? ["Person", "Organization", "Vessel", "Aircraft"] : ["Person", "Organization"]).includes(value)
  );

  const supports = (key: string) =>
    (internal ? cap?.profile_fields?.[profile]?.[entityType] ?? (profile === "exact-name-v1" ? cap?.fields?.[entityType] : []) : cap?.fields?.[entityType])?.includes(key) === true;

  const datasetSelection = cap?.dataset_selection === true;
  const identityValues: Record<string, string> = entityType === "Person"
    ? { birth_date: birthDate, nationality, address }
    : { country, registration_number: registration, address };

  const advanced = Object.keys(identityValues).some(supports) || datasetSelection;
  const incompatibleDraft = Object.entries(identityValues).some(([key, value]) => !!value.trim() && !supports(key)) ||
    (!!datasets.trim() && !datasetSelection) || (!!identifiers.length && !supports("identifiers"));

  const settingsSupported = entityTypes.includes(entityType) && supports("name") && !!cap &&
    (!internal || (selectedDatasets.length > 0 && (cap.profiles ?? ["exact-name-v1"]).includes(profile) && (profile === "exact-name-v1" || threshold >= 50))) &&
    threshold >= cap.threshold_min && threshold <= cap.threshold_max && limit >= 1 && limit <= cap.limit_max;
  const datasetSelectionError = internal && selectedDatasets.length === 0
    ? "Select at least one ready watchlist before searching."
    : undefined;

  function clearUnsupported() {
    if (!supports("identifiers")) setIdentifiers([]);
    if (entityType === "Person") {
      if (!supports("birth_date")) setBirthDate("");
      if (!supports("nationality")) setNationality("");
    } else {
      if (!supports("country")) setCountry("");
      if (!supports("registration_number")) setRegistration("");
    }
    if (!supports("address")) setAddress("");
    if (!datasetSelection) setDatasets("");
  }

  const thresholdOptions = Array.from(new Set([0, 40, 50, 60, 70, 80, 90, cap?.threshold_min, cap?.threshold_max, threshold]))
    .filter((v): v is number => typeof v === "number" && Number.isInteger(v)).sort((a, b) => a - b);

  const limitOptions = Array.from(new Set([1, 10, 20, 50, cap?.limit_max, limit]))
    .filter((v): v is number => typeof v === "number" && Number.isInteger(v)).sort((a, b) => a - b);

  const matches = result?.matches ?? [];
  const searchSummary = result?.query ?? submitted;
  const complete = result && !pending(result.status) && !["failed", "archived"].includes(result.status);

  return (
    <div className="mx-auto w-full max-w-[1480px] px-4 py-6 sm:px-6 lg:px-8 space-y-6">
      {/* Header and Top Navigation */}
      <header className="flex flex-wrap items-center justify-between gap-4 border-b border-slate-200 pb-5">
        <div>
          <div className="flex items-center gap-2 mb-1">
            <span className="inline-flex items-center gap-1.5 rounded-md bg-blue-50 px-2.5 py-1 text-xs font-semibold text-blue-700">
              <ShieldCheck className="size-4 text-blue-600" />
              Compliance Screening
            </span>
          </div>
          <h1 className="text-2xl font-bold text-slate-950 sm:text-3xl">Watchlist & Sanctions Search</h1>
          <p className="mt-1 text-sm text-slate-600">
            Screen individuals and organizations against global sanctions, PEP records, and watchlists.
          </p>
        </div>

        {/* Tab Navigation */}
        <div className="flex items-center gap-2.5">
          <Link
            href="/screening-search"
            className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-blue-600 bg-blue-50 px-3.5 text-sm font-semibold text-blue-900 shadow-2xs"
          >
            <Search className="size-4 text-blue-700" />
            Search
          </Link>
          <Link
            href="/screening-search/history"
            className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-3.5 text-sm font-medium text-slate-700 hover:bg-slate-50 transition-colors shadow-2xs"
          >
            <History className="size-4 text-slate-500" />
            Search history
          </Link>
          <Link
            href="/screening-search/lists"
            className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-3.5 text-sm font-medium text-slate-700 hover:bg-slate-50 transition-colors shadow-2xs"
          >
            <Tag className="size-4 text-slate-500" />
            Private lists
          </Link>
        </div>
      </header>

      {/* Two-Column Side-by-Side Layout with Comfortable Proportions */}
      <div className="grid items-start gap-6 lg:grid-cols-[360px_minmax(0,1fr)] xl:grid-cols-[380px_minmax(0,1fr)]">
        {/* Left Column: Search Form */}
        <form onSubmit={onSubmit} className="rounded-xl border border-slate-200 bg-white p-5 shadow-xs space-y-4">
          <div className="flex items-center justify-between pb-3 border-b border-slate-100">
            <div className="flex items-center gap-2">
              <div className="flex size-7 items-center justify-center rounded-lg bg-blue-50 text-blue-600">
                <Search className="size-4" />
              </div>
              <h2 className="text-sm font-bold text-slate-900">New search</h2>
            </div>
            <span className="text-xs text-slate-500">Manual screening</span>
          </div>

          {/* Provider Selection (Internal vs OpenSanctions) */}
          {providers.length > 1 && (
            <div className="flex items-center gap-1 rounded-lg bg-slate-100 p-1 text-xs">
              {providers.map(p => {
                const isSelected = provider === p.id;
                const isInternal = p.id === "internal-watchlists";
                return (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => {
                      setProvider(p.id);
                      if (isInternal && p.datasets) {
                        const ready = p.datasets.filter(d => !!d.generation).map(d => d.ref);
                        if (ready.length > 0) setSelectedDatasets(ready);
                      }
                    }}
                    className={clsx(
                      "flex-1 rounded-md py-1.5 px-2 text-center transition-all",
                      isSelected
                        ? "bg-white text-blue-700 shadow-2xs font-bold"
                        : "text-slate-600 hover:text-slate-900 font-medium"
                    )}
                  >
                    {isInternal ? "Internal Lists (Local)" : "OpenSanctions"}
                  </button>
                );
              })}
            </div>
          )}

          {/* User Messages and Status */}
          {refining && (
            <p role="status" className="rounded-lg border border-blue-200 bg-blue-50 px-3 py-2 text-xs text-blue-900 flex items-center gap-2">
              <RotateCcw className="size-4 text-blue-600 shrink-0" />
              <span>Stored settings copied. Edit them and search to create a separate run.</span>
            </p>
          )}

          {incompatibleDraft && (
            <div role="alert" className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2.5 text-xs text-amber-900 space-y-1">
              <p className="font-semibold">This connection cannot use some entered fields or datasets.</p>
              <button type="button" className="font-semibold underline hover:text-amber-950" onClick={clearUnsupported}>
                Clear unsupported settings
              </button>
            </div>
          )}

          {catalog.isError && (
            <div role="alert" className="rounded-lg border border-red-200 bg-red-50 px-3 py-2.5 text-xs text-red-800 flex items-center justify-between gap-2">
              <span>Provider settings unavailable.</span>
              <button type="button" className="font-semibold underline" onClick={() => void catalog.refetch()}>
                Retry provider settings
              </button>
            </div>
          )}

          {!cap?.configured && cap && (
            <div role="alert" className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2.5 text-xs text-amber-900 font-medium">
              Provider configuration required before searching
            </div>
          )}

          {cap?.configured && !catalogReady && !catalog.isError && (
            <p role="alert" className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-800 font-medium">
              Provider capabilities are incomplete or invalid. New searches are unavailable.
            </p>
          )}

          <fieldset disabled={busy || !catalogReady} className="space-y-4 disabled:opacity-60">
            {/* Entity Type Toggle */}
            <div>
              {/* Accessible select retained for test automation / screen readers (visually hidden) */}
              <label className="sr-only">
                Entity type
                <select
                  value={entityType}
                  onChange={e => setEntityType(e.target.value)}
                >
                  {!entityTypes.includes(entityType) && <option value={entityType} disabled>{entityType} (unsupported)</option>}
                  {entityTypes.map(value => <option key={value} value={value}>{value}</option>)}
                </select>
              </label>

              {/* Segmented Button with Clear Icons and Labels */}
              <div className="grid grid-cols-2 gap-2 p-1 bg-slate-100 rounded-lg border border-slate-200">
                <button
                  type="button"
                  onClick={() => setEntityType("Person")}
                  disabled={!entityTypes.includes("Person")}
                  className={`flex items-center justify-center gap-2 rounded-md py-2 text-xs font-semibold transition-all ${
                    entityType === "Person"
                      ? "bg-white text-blue-900 shadow-2xs border border-slate-200 ring-1 ring-blue-500/20"
                      : "text-slate-600 hover:text-slate-900"
                  }`}
                >
                  <User className={`size-4 ${entityType === "Person" ? "text-blue-600" : "text-slate-500"}`} />
                  <span>Person</span>
                </button>
                <button
                  type="button"
                  onClick={() => setEntityType("Organization")}
                  disabled={!entityTypes.includes("Organization")}
                  className={`flex items-center justify-center gap-2 rounded-md py-2 text-xs font-semibold transition-all ${
                    entityType === "Organization"
                      ? "bg-white text-blue-900 shadow-2xs border border-slate-200 ring-1 ring-blue-500/20"
                      : "text-slate-600 hover:text-slate-900"
                  }`}
                >
                  <Building2 className={`size-4 ${entityType === "Organization" ? "text-blue-600" : "text-slate-500"}`} />
                  <span>Organization</span>
                </button>
              </div>
            </div>

            {/* Name Input */}
            <div>
              <label className="block text-sm font-semibold text-slate-900">
                Name <span className="font-normal text-slate-500 text-xs">(Required)</span>
                <input
                  ref={nameInput}
                  required
                  maxLength={200}
                  autoComplete="off"
                  className={field}
                  value={name}
                  onChange={e => setName(e.target.value)}
                  placeholder={entityType === "Person" ? "Enter full name or alias" : "Enter company or organization name"}
                />
              </label>
            </div>

            {/* Additional Search Fields (Collapsible Accordion) */}
            {advanced && (
              <details className="group rounded-lg border border-slate-200 bg-slate-50/70 p-3 transition-all">
                <summary className="cursor-pointer text-xs font-semibold text-blue-700 hover:text-blue-800 flex items-center justify-between select-none">
                  <span className="flex items-center gap-1.5">
                    <SlidersHorizontal className="size-3.5 text-blue-600" />
                    Additional search fields
                  </span>
                  <span className="text-xs text-slate-400 group-open:rotate-180 transition-transform">▼</span>
                </summary>
                <div className="mt-3 space-y-3 pt-2.5 border-t border-slate-200">
                  {entityType === "Person" && supports("birth_date") && (
                    <div>
                      <label className="block text-xs font-semibold text-slate-800">
                        <span className="flex items-center gap-1.5">
                          <Calendar className="size-3.5 text-slate-500" />
                          Birth date
                        </span>
                        <div className="relative mt-1">
                          <input
                            className="w-full rounded-md border border-slate-300 bg-white px-3 py-1.5 pr-9 text-sm text-slate-900 focus:border-blue-600 focus:outline-none focus:ring-1 focus:ring-blue-500"
                            value={birthDate}
                            onChange={e => setBirthDate(e.target.value)}
                            placeholder="YYYY or YYYY-MM-DD"
                            maxLength={10}
                          />
                          <input
                            type="date"
                            tabIndex={-1}
                            aria-hidden="true"
                            className="sr-only"
                            ref={datePickerRef}
                            onChange={e => {
                              if (e.target.value) setBirthDate(e.target.value);
                            }}
                          />
                          <button
                            type="button"
                            onClick={() => datePickerRef.current?.showPicker?.()}
                            className="absolute right-2 top-1/2 -translate-y-1/2 p-1 text-slate-400 hover:text-slate-700 rounded transition-colors"
                            title="Open date picker"
                          >
                            <Calendar className="size-4" />
                          </button>
                        </div>
                      </label>
                    </div>
                  )}

                  {supports(entityType === "Person" ? "nationality" : "country") && (
                    <label className="block text-xs font-semibold text-slate-800">
                      <span className="flex items-center gap-1.5">
                        <Globe className="size-3.5 text-slate-500" />
                        {entityType === "Person" ? "Nationality" : "Country"}
                      </span>
                      <input
                        className={field}
                        value={entityType === "Person" ? nationality : country}
                        onChange={e => entityType === "Person" ? setNationality(e.target.value) : setCountry(e.target.value)}
                        placeholder="Two-letter code, e.g. GB"
                        minLength={2}
                        maxLength={2}
                        pattern="[A-Za-z]{2}"
                      />
                    </label>
                  )}

                  {entityType === "Organization" && supports("registration_number") && (
                    <label className="block text-xs font-semibold text-slate-800">
                      <span className="flex items-center gap-1.5">
                        <Fingerprint className="size-3.5 text-slate-500" />
                        Registration number
                      </span>
                      <input
                        className={field}
                        value={registration}
                        onChange={e => setRegistration(e.target.value)}
                        placeholder="Registration or Tax ID"
                        maxLength={100}
                      />
                    </label>
                  )}

                  {supports("address") && (
                    <label className="block text-xs font-semibold text-slate-800">
                      <span className="flex items-center gap-1.5">
                        <MapPin className="size-3.5 text-slate-500" />
                        Address
                      </span>
                      <input
                        className={field}
                        value={address}
                        onChange={e => setAddress(e.target.value)}
                        placeholder="City or location"
                        maxLength={300}
                      />
                    </label>
                  )}

                  {datasetSelection && !internal && (
                    <label className="block text-xs font-semibold text-slate-800">
                      <span className="flex items-center gap-1.5">
                        <FileText className="size-3.5 text-slate-500" />
                        Dataset IDs
                      </span>
                      <input
                        className={field}
                        value={datasets}
                        onChange={e => setDatasets(e.target.value)}
                        placeholder="us_ofac_sdn, gb_hmt_sanctions"
                        maxLength={810}
                      />
                    </label>
                  )}
                </div>
              </details>
            )}

            {supports("identifiers") && <IdentifierFields rows={identifiers} onChange={setIdentifiers} />}

            {/* Internal provider specifics if active */}
            {internal && (
              <div className="space-y-2.5 rounded-lg border border-slate-200 bg-slate-50/70 p-3">
                <label className="block text-xs font-semibold text-slate-800">
                  <span>Matching method</span>
                  <select
                    className={field}
                    value={profile}
                    onChange={event => {
                      setProfile(event.target.value);
                      setThreshold(event.target.value !== "exact-name-v1" ? 85 : 100);
                    }}
                  >
                    {(cap?.profiles ?? ["exact-name-v1"]).map(value => (
                      <option value={value} key={value}>
                        {value === "identifier-evidence-v1" ? "Names and exact identifiers" : value === "name-token-v2" ? "Similar names with token checks" : value === "name-fuzzy-v1" ? "Similar names" : "Exact names and aliases"}
                      </option>
                    ))}
                  </select>
                </label>

                <fieldset className="space-y-1.5 rounded-md border border-slate-200 bg-white p-2.5">
                  <legend className="px-1 text-xs font-semibold text-slate-800">
                    Watchlists <span className="font-normal text-slate-500">({selectedDatasets.length} selected)</span>
                  </legend>
                  <div className="flex items-center justify-between gap-2">
                    <p className="text-[11px] text-slate-500">Search all ready watchlists or choose specific lists.</p>
                    <div className="flex shrink-0 gap-2">
                      <button type="button" className="text-xs font-semibold text-blue-700" onClick={() => setSelectedDatasets((cap?.datasets ?? []).filter(dataset => !!dataset.generation).map(dataset => dataset.ref))}>Select all</button>
                      <button type="button" className="text-xs font-semibold text-slate-600" onClick={() => setSelectedDatasets([])}>Clear all</button>
                    </div>
                  </div>
                  <div className="max-h-32 space-y-1.5 overflow-y-auto pr-1">
                    {cap?.datasets?.map(dataset => (
                      <label className="flex items-start gap-2 text-xs" key={`${dataset.ref.scope}:${dataset.ref.id}`}>
                        <input
                          type="checkbox"
                          className="mt-0.5 size-3.5 accent-blue-700 rounded"
                          disabled={!dataset.generation && !selectedDatasets.some(ref => ref.id === dataset.ref.id && ref.scope === dataset.ref.scope)}
                          checked={selectedDatasets.some(ref => ref.id === dataset.ref.id && ref.scope === dataset.ref.scope)}
                          onChange={event => setSelectedDatasets(previous => event.target.checked ? [...previous, dataset.ref] : previous.filter(ref => ref.id !== dataset.ref.id || ref.scope !== dataset.ref.scope))}
                        />
                        <span className="min-w-0">
                          <span className="font-semibold text-slate-800">{formatDatasetName(dataset.name)}</span>
                          <span className="block text-[11px] text-slate-500">
                            {dataset.ref.scope === "tenant" ? "Private list" : dataset.category.replaceAll("_", " ")}
                            {!dataset.generation ? " · Not ready" : ""}
                          </span>
                        </span>
                      </label>
                    ))}
                  </div>
                </fieldset>
              </div>
            )}

            {/* Threshold & Result Limit Bar */}
            <div className="grid grid-cols-2 gap-2.5 pt-1 border-t border-slate-100">
              <label className="block text-xs font-semibold text-slate-800">
                <span>Matching threshold</span>
                <select
                  className={field}
                  value={threshold}
                  onChange={e => setThreshold(Number(e.target.value))}
                >
                  {thresholdOptions.map(value => (
                    <option key={value} value={value} disabled={!cap || value < cap.threshold_min || value > cap.threshold_max}>
                      {value}%
                    </option>
                  ))}
                </select>
              </label>

              <label className="block text-xs font-semibold text-slate-800">
                <span>Result limit</span>
                <select
                  className={field}
                  value={limit}
                  onChange={e => setLimit(Number(e.target.value))}
                >
                  {limitOptions.map(value => (
                    <option key={value} disabled={!cap || value > cap.limit_max}>
                      {value}
                    </option>
                  ))}
                </select>
              </label>
            </div>

            {catalogReady && !settingsSupported && (
              <p role="alert" className="text-xs font-medium text-amber-900 bg-amber-50 p-2.5 rounded-md border border-amber-200">
                {datasetSelectionError ?? "Choose a supported entity type, matching threshold and result limit before searching."}
              </p>
            )}

            {/* Submit Button */}
            <Button
              type="submit"
              disabled={incompatibleDraft || !settingsSupported}
              className="h-10 w-full rounded-lg text-sm font-semibold shadow-xs"
            >
              {busy ? (
                <>
                  <LoaderCircle className="size-4 animate-spin mr-1.5" />
                  Searching…
                </>
              ) : (
                <>
                  <Search className="size-4 mr-1.5" />
                  Search
                </>
              )}
            </Button>
          </fieldset>
        </form>

        {/* Right Column: Search Results */}
        <section aria-label="Search results" aria-busy={busy} className="min-w-0 space-y-4">
          {complete && result.query?.provider === "internal-watchlists" && (
            <SourceMonitor searchId={result.id} scope={scope} />
          )}

          {/* Results Summary Bar */}
          {searchSummary && result && !busy && (
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-slate-200 bg-white px-4 py-3.5 shadow-2xs">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  {historical && (
                    <span className="inline-flex items-center rounded-md bg-blue-50 px-2 py-0.5 text-xs font-semibold text-blue-700">
                      Stored search
                    </span>
                  )}
                  <span className="text-sm text-slate-700">
                    {historical ? "Stored results for " : "Results for "}
                    <strong className="font-bold text-slate-950 text-base">{searchSummary.name}</strong>
                    <span className="text-slate-500 text-xs"> · {searchSummary.entity_type} · threshold {searchSummary.threshold}%</span>
                  </span>
                </div>
                {[searchSummary.birth_date, searchSummary.nationality, searchSummary.country, searchSummary.registration_number, searchSummary.address, searchSummary.datasets?.join(", ")].filter(Boolean).length > 0 && (
                  <div className="mt-1.5 flex flex-wrap gap-2 text-xs text-slate-600">
                    {[searchSummary.birth_date, searchSummary.nationality, searchSummary.country, searchSummary.registration_number, searchSummary.address, searchSummary.datasets?.join(", ")].filter(Boolean).map((value, index) => (
                      <span className="inline-flex items-center rounded-md bg-slate-100 px-2.5 py-0.5 text-xs text-slate-700 font-medium break-words" key={index}>
                        {value}
                      </span>
                    ))}
                  </div>
                )}
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <Link
                  href={`/screening-search/${result.id}/report`}
                  className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-3 text-xs font-semibold text-blue-700 hover:bg-blue-50 transition-colors shadow-2xs"
                >
                  <FileText className="size-3.5" />
                  View printable report
                </Link>
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 rounded-lg px-3 text-xs font-semibold shadow-2xs"
                  disabled={!catalogReady}
                  onClick={refine}
                >
                  <RotateCcw className="size-3.5 mr-1" />
                  Refine as new search
                </Button>
              </div>
            </div>
          )}

          {historical && (
            <p className="rounded-lg border border-blue-200 bg-blue-50/70 px-3.5 py-2 text-xs text-blue-900 font-medium">
              Stored search results · no new provider search{result?.created_at ? ` · ${new Date(result.created_at).toLocaleString()}` : ""}
            </p>
          )}

          {error && (
            <div role="alert" className="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-800 space-y-2">
              <p className="font-semibold">{error}</p>
              {(submitted || result) && (
                <Button
                  type="button"
                  variant="outline"
                  className="mt-1 h-8 rounded-lg px-3 text-xs font-semibold bg-white"
                  disabled={sending}
                  onClick={() => result ? (setError(""), setRefresh(value => value + 1)) : void submit(submitted!)}
                >
                  {result ? "Check status again" : "Retry same search"}
                </Button>
              )}
            </div>
          )}

          {busy && (
            <div role="status" className="flex items-center gap-3.5 rounded-xl border border-slate-200 bg-white p-7 text-sm text-slate-700 shadow-2xs">
              <LoaderCircle className="size-5 animate-spin text-blue-600 shrink-0" />
              <div className="space-y-0.5">
                <p className="font-semibold text-slate-900">Searching provider data…</p>
                <p className="text-xs text-slate-500">Querying watchlists and screening records.</p>
              </div>
            </div>
          )}

          {!submitted && !result && (
            <div className="rounded-2xl border border-dashed border-slate-300 bg-white p-12 text-center shadow-2xs">
              <div className="mx-auto flex size-12 items-center justify-center rounded-2xl bg-blue-50 text-blue-600 mb-3">
                <Search className="size-6" />
              </div>
              <h2 className="text-base font-bold text-slate-900">Screen a person or organization</h2>
              <p className="mt-1 max-w-sm mx-auto text-xs text-slate-500 leading-relaxed">
                Enter a name on the left and choose matching settings to begin screening.
              </p>
            </div>
          )}

          {result?.status === "failed" && (
            <div role="alert" className="rounded-xl border border-red-200 bg-red-50 p-4 text-xs text-red-800 font-medium">
              The provider could not complete this search. You can submit a new search using the form.
            </div>
          )}

          {result?.status === "archived" && (
            <p role="status" className="rounded-xl border border-slate-200 bg-white p-4 text-xs text-slate-600 font-medium">
              This search has been archived.
            </p>
          )}

          {result?.partial && (
            <div role="status" className="flex flex-wrap items-start justify-between gap-3 rounded-xl border border-amber-200 bg-amber-50 p-3.5 text-xs text-amber-950">
              <div className="flex items-start gap-2">
                <AlertTriangle className="size-4 text-amber-600 shrink-0 mt-0.5" />
                <p>
                  <strong>Incomplete results.</strong> Additional candidates may exist beyond this response. Refine the name or increase the result limit and search again.
                </p>
              </div>
              {result.query && (
                <button type="button" className="shrink-0 font-bold underline hover:text-amber-900" onClick={refine}>
                  Refine search
                </button>
              )}
            </div>
          )}

          {/* Results Candidate List (Tabular View) */}
          {complete && (
            <div className="rounded-xl border border-slate-200 bg-white shadow-xs overflow-hidden">
              {/* Header of results list */}
              <div className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-200 px-5 py-3.5 bg-slate-50/70">
                <div>
                  <h2 className="text-base font-bold text-slate-950">
                    {matches.length} potential {matches.length === 1 ? "match" : "matches"}
                  </h2>
                  <p className="text-xs text-slate-500">
                    Similarity supports review and does not confirm identity. Sorted by matching score.
                  </p>
                </div>
                {matches.length > 0 && (
                  <div className="flex items-center gap-2">
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="h-7 text-xs font-semibold text-slate-600 hover:text-slate-900"
                      onClick={() => {
                        if (expandedMatches.size === matches.length) {
                          setExpandedMatches(new Set());
                        } else {
                          setExpandedMatches(new Set(matches.map(m => m.id)));
                        }
                      }}
                    >
                      <Layers className="size-3.5 mr-1" />
                      {expandedMatches.size === matches.length ? "Collapse All" : "Expand All Details"}
                    </Button>
                    <span className="text-xs text-slate-400">|</span>
                    <span className="text-xs font-medium text-slate-500">Sorted by score</span>
                  </div>
                )}
              </div>

              {matches.length === 0 ? (
                <div className="px-5 py-12 text-center">
                  <ShieldCheck className="mx-auto mb-2 size-7 text-slate-400" />
                  <p className="text-sm font-semibold text-slate-800">
                    {result.partial ? "No candidates returned in this incomplete response." : "No matches found for these search settings."}
                  </p>
                  {result.partial && (
                    <p className="mt-1 text-xs text-slate-500">
                      This does not establish a negative screening.
                    </p>
                  )}
                </div>
              ) : (
                <div className="overflow-x-auto">
                  <table className="w-full text-left text-xs border-collapse">
                    <thead>
                      <tr className="border-b border-slate-200 bg-slate-50/90 text-[11px] font-bold text-slate-600 uppercase tracking-wider select-none">
                        <th className="py-2.5 px-3 w-9 text-center"></th>
                        <th className="py-2.5 px-3 min-w-[200px]">Candidate / Entity</th>
                        <th className="py-2.5 px-3 min-w-[140px]">Risk Category</th>
                        <th className="py-2.5 px-3 min-w-[220px]">Role / Relationship Context</th>
                        <th className="py-2.5 px-3 min-w-[160px]">Watchlist Source</th>
                        <th className="py-2.5 px-3 w-28 text-center">Score</th>
                        <th className="py-2.5 px-3 w-36 text-right">Investigation</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-150">
                      {matches.map(match => {
                        const ctx = parseCandidateContext(match);
                        const isExpanded = expandedMatches.has(match.id);
                        const exactMatch = match.properties.matchingProfile?.includes("exact-name-v1") === true || match.score >= 0.999;
                        const similarityScore = Math.round(match.score * 100);
                        const isOrg = match.schema?.toLowerCase().includes("organization") || match.schema?.toLowerCase().includes("company");

                        const borderAccent = ctx.isRca
                          ? "border-l-4 border-l-purple-500 hover:bg-purple-50/30"
                          : ctx.isPep
                          ? "border-l-4 border-l-amber-500 hover:bg-amber-50/30"
                          : (ctx.isSanctioned || ctx.isDebarred)
                          ? "border-l-4 border-l-rose-500 hover:bg-rose-50/30"
                          : "border-l-4 border-l-blue-400 hover:bg-slate-50/50";

                        return (
                          <Fragment key={match.id}>
                            <tr
                              className={`transition-colors cursor-pointer ${borderAccent} ${isExpanded ? "bg-slate-50/90" : "bg-white"}`}
                              onClick={() => toggleExpand(match.id)}
                            >
                              {/* Col 1: Chevron toggle */}
                              <td className="py-3 px-3 text-center align-middle">
                                <button
                                  type="button"
                                  className="inline-flex size-6 items-center justify-center rounded text-slate-400 hover:text-slate-700 hover:bg-slate-200/60 transition-colors"
                                  aria-label={isExpanded ? "Collapse row" : "Expand row"}
                                  onClick={e => {
                                    e.stopPropagation();
                                    toggleExpand(match.id);
                                  }}
                                >
                                  {isExpanded ? <ChevronUp className="size-4" /> : <ChevronDown className="size-4" />}
                                </button>
                              </td>

                              {/* Col 2: Candidate / Entity */}
                              <td className="py-3 px-3 align-middle">
                                <div className="flex items-center gap-2.5 min-w-0">
                                  <div className={`flex size-8 shrink-0 items-center justify-center rounded-lg ${
                                    isOrg
                                      ? "bg-emerald-100 text-emerald-800"
                                      : ctx.isRca
                                      ? "bg-purple-100 text-purple-800"
                                      : ctx.isPep
                                      ? "bg-amber-100 text-amber-800"
                                      : "bg-blue-100 text-blue-800"
                                  }`}>
                                    {isOrg ? <Building2 className="size-4" /> : ctx.isRca ? <Users className="size-4" /> : <User className="size-4" />}
                                  </div>
                                  <div className="min-w-0">
                                    <div className="font-bold text-slate-900 text-sm leading-tight truncate">
                                      {match.name || match.entity_id}
                                    </div>
                                    <div className="flex items-center gap-1.5 mt-0.5">
                                      <span className="text-[10px] font-semibold text-slate-600 bg-slate-100 px-1.5 py-0.2 rounded">
                                        {match.schema || submitted?.entity_type}
                                      </span>
                                      {match.entity_id && (
                                        <span className="text-[10px] text-slate-400 font-mono truncate max-w-[120px]" title={match.entity_id}>
                                          {match.entity_id}
                                        </span>
                                      )}
                                    </div>
                                  </div>
                                </div>
                              </td>

                              {/* Col 3: Risk Category */}
                              <td className="py-3 px-3 align-middle whitespace-nowrap">
                                {ctx.isPep && (
                                  <span className="inline-flex items-center gap-1 rounded-full bg-amber-50 border border-amber-300 px-2.5 py-1 text-[11px] font-bold text-amber-900 shadow-2xs">
                                    <ShieldAlert className="size-3 text-amber-600" />
                                    PEP {ctx.statusBadge === "Cooling-Off" ? "(Cooling-Off)" : ""}
                                  </span>
                                )}
                                {ctx.isRca && (
                                  <span className="inline-flex items-center gap-1 rounded-full bg-purple-50 border border-purple-300 px-2.5 py-1 text-[11px] font-bold text-purple-900 shadow-2xs">
                                    <Users className="size-3 text-purple-600" />
                                    RCA {ctx.statusBadge === "Cooling-Off" ? "(Cooling-Off)" : ""}
                                  </span>
                                )}
                                {ctx.isSanctioned && !ctx.isPep && !ctx.isRca && (
                                  <span className="inline-flex items-center gap-1 rounded-full bg-rose-50 border border-rose-300 px-2.5 py-1 text-[11px] font-bold text-rose-900 shadow-2xs">
                                    <AlertCircle className="size-3 text-rose-600" />
                                    Sanctioned
                                  </span>
                                )}
                                {ctx.isDebarred && !ctx.isPep && !ctx.isRca && !ctx.isSanctioned && (
                                  <span className="inline-flex items-center gap-1 rounded-full bg-orange-50 border border-orange-300 px-2.5 py-1 text-[11px] font-bold text-orange-900 shadow-2xs">
                                    <ShieldAlert className="size-3 text-orange-600" />
                                    Debarred Firm
                                  </span>
                                )}
                                {!ctx.isPep && !ctx.isRca && !ctx.isSanctioned && !ctx.isDebarred && (
                                  <span className="inline-flex items-center gap-1 rounded-full bg-slate-100 border border-slate-300 px-2 py-0.5 text-[11px] font-medium text-slate-700">
                                    Watchlist Hit
                                  </span>
                                )}
                              </td>

                              {/* Col 4: Role / Relationship Context */}
                              <td className="py-3 px-3 align-middle">
                                {ctx.isRca ? (
                                  <div className="space-y-0.5">
                                    {ctx.connectedPep && (
                                      <div className="text-xs font-semibold text-purple-950 flex items-center gap-1">
                                        <span className="text-purple-600 text-[11px] font-normal">Connected:</span>
                                        <span className="truncate max-w-[200px]" title={ctx.connectedPep}>{ctx.connectedPep}</span>
                                      </div>
                                    )}
                                    {ctx.relationship && (
                                      <span className="inline-block rounded-md bg-purple-100 text-purple-800 px-1.5 py-0.5 text-[11px] font-semibold">
                                        {ctx.relationship}
                                      </span>
                                    )}
                                  </div>
                                ) : ctx.isPep ? (
                                  <div className="max-w-sm whitespace-normal break-words text-xs font-medium text-slate-800" title={ctx.role}>
                                    {ctx.role || "Role not provided by source"}
                                  </div>
                                ) : ctx.isDebarred ? (
                                  <div className="text-xs text-slate-700 truncate max-w-[240px]" title={ctx.complianceNotice || ctx.role}>
                                    {ctx.complianceNotice || ctx.role || "Ineligible Entity"}
                                  </div>
                                ) : (
                                  <div className="text-xs text-slate-700 truncate max-w-[240px]" title={ctx.complianceNotice || match.properties.programs?.[0]}>
                                    {ctx.complianceNotice || match.properties.programs?.[0] || "Listed Entity"}
                                  </div>
                                )}
                              </td>

                              {/* Col 5: Watchlist Source */}
                              <td className="py-3 px-3 align-middle">
                                <div className="flex flex-wrap items-center gap-1">
                                  {match.datasets?.map(dataset => (
                                    <span
                                      key={dataset}
                                      className="inline-flex items-center gap-1 rounded bg-slate-100 border border-slate-200/80 px-2 py-0.5 text-[11px] font-semibold text-slate-700"
                                      title={dataset}
                                    >
                                      <Tag className="size-2.5 text-slate-500" />
                                      {formatDatasetName(dataset)}
                                    </span>
                                  ))}
                                </div>
                              </td>

                              {/* Col 6: Score */}
                              <td className="py-3 px-3 align-middle text-center whitespace-nowrap">
                                {exactMatch ? (
                                  <span className="inline-flex items-center gap-1 rounded-full border border-emerald-300 bg-emerald-50 px-2 py-0.5 text-xs font-bold text-emerald-800">
                                    <CheckCircle2 className="size-3 text-emerald-600" />
                                    100%
                                  </span>
                                ) : (
                                  <span className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-bold tabular-nums ${
                                    similarityScore >= 85
                                      ? "border-amber-300 bg-amber-50 text-amber-900"
                                      : "border-blue-300 bg-blue-50 text-blue-900"
                                  }`}>
                                    <span className={`size-1.5 rounded-full ${similarityScore >= 85 ? "bg-amber-600" : "bg-blue-600"}`} />
                                    {similarityScore}%
                                  </span>
                                )}
                              </td>

                              {/* Col 7: Investigation Action */}
                              <td className="py-3 px-3 align-middle text-right whitespace-nowrap" onClick={e => e.stopPropagation()}>
                                <OpenInvestigation
                                  tenant={scope.split(":")[0]}
                                  scope={scope}
                                  screening={result.id}
                                  candidate={match.id}
                                />
                              </td>
                            </tr>

                            {/* Expandable Details Drawer */}
                            {isExpanded && (
                              <tr className="bg-slate-50/80 border-b border-slate-200">
                                <td colSpan={7} className="p-4 pl-12">
                                  <div className="grid gap-3 rounded-xl border border-slate-200/90 bg-white p-4 shadow-sm sm:grid-cols-2 lg:grid-cols-3">
                                    {/* Panel 1: Identity & Demographics */}
                                    <div className="space-y-2 rounded-lg bg-slate-50 p-3 border border-slate-200/70">
                                      <div className="flex items-center gap-1.5 pb-1 border-b border-slate-200 font-bold text-xs text-slate-800">
                                        <User className="size-3.5 text-slate-500" />
                                        Identity & Demographics
                                      </div>
                                      <dl className="space-y-1.5 text-xs">
                                        {ctx.aliases.length > 0 && (
                                          <div>
                                            <dt className="text-[11px] font-semibold text-slate-500">Aliases & Name Variants</dt>
                                            <dd className="flex flex-wrap gap-1 mt-0.5">
                                              {ctx.aliases.map((alias, i) => (
                                                <span key={i} className="inline-block rounded bg-white border border-slate-200 px-1.5 py-0.5 text-[11px] font-medium text-slate-800">
                                                  {alias}
                                                </span>
                                              ))}
                                            </dd>
                                          </div>
                                        )}
                                        {match.properties.birthDate?.[0] && (
                                          <div className="flex items-center gap-1.5">
                                            <Calendar className="size-3 text-slate-400" />
                                            <span className="text-slate-500 font-medium">Birth Date:</span>
                                            <span className="font-semibold text-slate-900">{match.properties.birthDate[0]}</span>
                                          </div>
                                        )}
                                        {(match.properties.nationalityText?.[0] || match.properties.countryText?.[0]) && (
                                          <div className="flex items-center gap-1.5">
                                            <Globe className="size-3 text-slate-400" />
                                            <span className="text-slate-500 font-medium">Nationality / Country:</span>
                                            <span className="font-semibold text-slate-900">
                                              {[match.properties.nationalityText?.[0], match.properties.countryText?.[0]].filter(Boolean).join(" · ")}
                                            </span>
                                          </div>
                                        )}
                                        {match.properties.address?.[0] && (
                                          <div className="flex items-start gap-1.5">
                                            <MapPin className="size-3 text-slate-400 mt-0.5 shrink-0" />
                                            <span className="text-slate-500 font-medium shrink-0">Address:</span>
                                            <span className="font-semibold text-slate-900 break-words">{match.properties.address[0]}</span>
                                          </div>
                                        )}
                                        <div>
                                          <dt className="text-[11px] font-semibold text-slate-500">Record ID</dt>
                                          <dd className="font-mono text-[11px] text-slate-600 select-all">{match.entity_id}</dd>
                                        </div>
                                      </dl>
                                    </div>

                                    {/* Panel 2: Relationships & Political Graph */}
                                    {(ctx.isPep || ctx.isRca || ctx.family.length > 0 || ctx.associates.length > 0) && (
                                      <div className="space-y-2 rounded-lg bg-purple-50/50 p-3 border border-purple-200/70">
                                        <div className="flex items-center gap-1.5 pb-1 border-b border-purple-200 font-bold text-xs text-purple-900">
                                          <Users className="size-3.5 text-purple-600" />
                                          Relationships & Political Graph
                                        </div>
                                        <dl className="space-y-2 text-xs">
                                          {ctx.connectedPep && (
                                            <div>
                                              <dt className="text-[11px] font-semibold text-purple-700">Connected Politically Exposed Person</dt>
                                              <dd className="font-bold text-purple-950 mt-0.5">{ctx.connectedPep}</dd>
                                              {ctx.relationship && (
                                                <span className="inline-block mt-1 rounded bg-purple-200/70 text-purple-900 px-2 py-0.5 text-[11px] font-semibold">
                                                  Relationship: {ctx.relationship}
                                                </span>
                                              )}
                                            </div>
                                          )}
                                          {ctx.family.length > 0 && (
                                            <div>
                                              <dt className="text-[11px] font-semibold text-slate-600">Immediate Family Members</dt>
                                              <dd className="text-slate-900 font-medium mt-0.5 text-[11px] leading-relaxed">
                                                {ctx.family.join(" · ")}
                                              </dd>
                                            </div>
                                          )}
                                          {ctx.associates.length > 0 && (
                                            <div>
                                              <dt className="text-[11px] font-semibold text-slate-600">Known Associates / Key Aides</dt>
                                              <dd className="text-slate-900 font-medium mt-0.5 text-[11px] leading-relaxed">
                                                {ctx.associates.join(" · ")}
                                              </dd>
                                            </div>
                                          )}
                                        </dl>
                                      </div>
                                    )}

                                    {/* Panel 3: Compliance & Screening Directives */}
                                    <div className="space-y-2 rounded-lg bg-amber-50/50 p-3 border border-amber-200/70">
                                      <div className="flex items-center gap-1.5 pb-1 border-b border-amber-200 font-bold text-xs text-amber-900">
                                        <ShieldCheck className="size-3.5 text-amber-600" />
                                        Compliance & Screening Directives
                                      </div>
                                      <dl className="space-y-2 text-xs">
                                        <div>
                                          <dt className="text-[11px] font-semibold text-amber-800">Directives & Obligations</dt>
                                          <dd className="font-semibold text-amber-950 mt-0.5 leading-snug">
                                            {ctx.complianceNotice || "Standard screening review required."}
                                          </dd>
                                        </div>
                                        {match.properties.listedAt?.[0] && (
                                          <div>
                                            <dt className="text-[11px] font-semibold text-slate-500">Effective Date</dt>
                                            <dd className="text-slate-800 font-medium">{match.properties.listedAt[0]}</dd>
                                          </div>
                                        )}
                                        {match.properties.matchingStrategy?.[0] && (
                                          <div>
                                            <dt className="text-[11px] font-semibold text-slate-500">Match Evidence</dt>
                                            <dd className="text-[11px] text-slate-700">
                                              Strategy: <span className="font-semibold">{match.properties.matchingStrategy[0]}</span>
                                              {match.properties.comparisonValues?.[0] && (
                                                <div className="font-mono text-[10px] text-slate-600 mt-0.5">
                                                  {match.properties.comparisonValues[0]}
                                                </div>
                                              )}
                                            </dd>
                                          </div>
                                        )}
                                      </dl>
                                    </div>
                                    <div className="space-y-2 rounded-lg border border-slate-200 bg-slate-50 p-3 sm:col-span-2 lg:col-span-3">
                                      <div className="flex items-center gap-1.5 border-b border-slate-200 pb-1 text-xs font-bold text-slate-800">
                                        <FileText className="size-3.5 text-slate-500" />
                                        Source evidence
                                      </div>
                                      <dl className="grid gap-3 text-xs sm:grid-cols-2">
                                        {match.properties.source?.length > 0 && <div><dt className="text-[11px] font-semibold text-slate-500">Source reference</dt><dd className="mt-0.5 break-words text-slate-800">{match.properties.source.join(" · ")}</dd></div>}
                                        {match.properties.sourceRecord?.length > 0 && <div><dt className="text-[11px] font-semibold text-slate-500">Source record</dt><dd className="mt-0.5 break-all font-mono text-slate-800">{match.properties.sourceRecord.join(" · ")}</dd></div>}
                                        {match.properties.datasetVersion?.length > 0 && <div><dt className="text-[11px] font-semibold text-slate-500">Dataset version</dt><dd className="mt-0.5 break-all font-mono text-slate-800">{match.properties.datasetVersion.join(" · ")}</dd></div>}
                                        {match.properties.sourceStatus?.length > 0 && <div><dt className="text-[11px] font-semibold text-slate-500">Status recorded by source</dt><dd className="mt-0.5 break-words text-slate-800">{match.properties.sourceStatus.join(" · ")}</dd></div>}
                                        {match.properties.programs?.length > 0 && <div><dt className="text-[11px] font-semibold text-slate-500">Classification and source labels</dt><dd className="mt-0.5"><ul className="list-inside list-disc space-y-0.5 text-slate-800">{match.properties.programs.map((value, index) => <li key={index}>{value}</li>)}</ul></dd></div>}
                                      </dl>
                                    </div>
                                  </div>
                                </td>
                              </tr>
                            )}
                          </Fragment>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          )}
        </section>
      </div>
    </div>
  );
}
