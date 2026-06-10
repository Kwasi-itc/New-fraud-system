import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export default function InvestigatorPage() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Investigator</CardTitle>
        <CardDescription>
          Analyst workspace for timelines, identity links, and evidence review.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4 lg:grid-cols-[1.05fr_0.95fr]">
        <div className="rounded-[24px] border border-border bg-muted/60 p-5">
          <p className="text-xs font-semibold tracking-[0.24em] text-muted-foreground">
            CURRENT INVESTIGATION
          </p>
          <p className="mt-4 text-xl font-semibold">
            Account takeover cluster around newly enrolled devices.
          </p>
          <p className="mt-3 text-sm leading-7 text-muted-foreground">
            Multiple identities share device fingerprints, recovery attempts,
            and checkout velocity patterns across two regions.
          </p>
        </div>
        <div className="rounded-[24px] border border-border bg-muted/60 p-5">
          <p className="text-xs font-semibold tracking-[0.24em] text-muted-foreground">
            NEXT ACTIONS
          </p>
          <ul className="mt-4 space-y-3 text-sm leading-7 text-muted-foreground">
            <li>Correlate card tokens with merchant exposure.</li>
            <li>Compare recovery flows against trusted-device history.</li>
            <li>Package evidence for escalation to operations.</li>
          </ul>
        </div>
      </CardContent>
    </Card>
  );
}
