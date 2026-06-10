import { ArrowUpRight, ShieldAlert, Siren, TimerReset } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";

const kpis = [
  {
    label: "Transactions screened",
    value: "14.2M",
    change: "+8.4%",
  },
  {
    label: "Analyst response SLA",
    value: "06:12",
    change: "-41s",
  },
  {
    label: "Confirmed fraud loss",
    value: "$42.8K",
    change: "-12.6%",
  },
];

const queues = [
  {
    name: "Card testing burst",
    status: "Critical",
    region: "Accra / Lagos",
    count: 18,
  },
  {
    name: "Account takeover review",
    status: "High",
    region: "London / Remote",
    count: 7,
  },
  {
    name: "Merchant anomaly watch",
    status: "Normal",
    region: "Johannesburg",
    count: 4,
  },
];

export default function DashboardPage() {
  return (
    <div className="space-y-6">
      <section className="grid gap-4 xl:grid-cols-[1.35fr_0.65fr]">
        <Card className="overflow-hidden border-black bg-black text-white">
          <CardContent className="relative p-8">
            <div className="absolute inset-0 bg-[radial-gradient(circle_at_top_right,rgba(37,99,235,0.35),transparent_32%)]" />
            <div className="relative flex flex-col gap-8">
              <div className="flex flex-wrap items-center gap-3">
                <Badge variant="accent">Realtime defense</Badge>
                <Badge className="border-white/12 bg-white/8 text-white">
                  June 9, 2026
                </Badge>
              </div>
              <div className="max-w-3xl space-y-3">
                <h2 className="text-4xl font-semibold tracking-tight">
                  Your fraud pressure is concentrated in card-not-present
                  traffic, but case throughput is holding.
                </h2>
                <p className="max-w-2xl text-base leading-7 text-zinc-300">
                  Use this workspace to balance signal quality, operational
                  speed, and approval friction across teams and geographies.
                </p>
              </div>
              <div className="grid gap-4 md:grid-cols-3">
                {kpis.map((item) => (
                  <div
                    key={item.label}
                    className="rounded-[24px] border border-white/10 bg-white/6 p-5"
                  >
                    <p className="text-sm text-zinc-400">{item.label}</p>
                    <div className="mt-3 flex items-end justify-between gap-4">
                      <p className="text-3xl font-semibold">{item.value}</p>
                      <p className="text-sm font-medium text-blue-200">
                        {item.change}
                      </p>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Response posture</CardTitle>
            <CardDescription>
              Current health across detection, review, and intervention.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-5">
            <div className="flex items-center justify-between rounded-[24px] bg-muted p-4">
              <div className="flex items-center gap-3">
                <div className="flex size-11 items-center justify-center rounded-2xl bg-red-50 text-red-600">
                  <ShieldAlert className="size-5" />
                </div>
                <div>
                  <p className="font-medium">Escalations pending</p>
                  <p className="text-sm text-muted-foreground">4 cases</p>
                </div>
              </div>
              <ArrowUpRight className="size-4 text-muted-foreground" />
            </div>
            <div className="flex items-center justify-between rounded-[24px] bg-muted p-4">
              <div className="flex items-center gap-3">
                <div className="flex size-11 items-center justify-center rounded-2xl bg-blue-50 text-blue-600">
                  <Siren className="size-5" />
                </div>
                <div>
                  <p className="font-medium">Signal drift notices</p>
                  <p className="text-sm text-muted-foreground">2 models</p>
                </div>
              </div>
              <ArrowUpRight className="size-4 text-muted-foreground" />
            </div>
            <div className="flex items-center justify-between rounded-[24px] bg-muted p-4">
              <div className="flex items-center gap-3">
                <div className="flex size-11 items-center justify-center rounded-2xl bg-emerald-50 text-emerald-600">
                  <TimerReset className="size-5" />
                </div>
                <div>
                  <p className="font-medium">Playbooks executed</p>
                  <p className="text-sm text-muted-foreground">17 today</p>
                </div>
              </div>
              <ArrowUpRight className="size-4 text-muted-foreground" />
            </div>
          </CardContent>
        </Card>
      </section>

      <section className="grid gap-4 xl:grid-cols-[0.95fr_1.05fr]">
        <Card>
          <CardHeader>
            <CardTitle>Priority queues</CardTitle>
            <CardDescription>
              Analyst workloads needing immediate attention.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {queues.map((queue, index) => (
              <div key={queue.name}>
                <div className="flex items-center justify-between gap-4 py-1">
                  <div>
                    <p className="font-medium">{queue.name}</p>
                    <p className="mt-1 text-sm text-muted-foreground">
                      {queue.region}
                    </p>
                  </div>
                  <div className="text-right">
                    <Badge
                      variant={
                        queue.status === "Critical"
                          ? "warning"
                          : queue.status === "High"
                            ? "accent"
                            : "success"
                      }
                    >
                      {queue.status}
                    </Badge>
                    <p className="mt-2 text-sm font-medium">{queue.count} open</p>
                  </div>
                </div>
                {index < queues.length - 1 ? <Separator className="mt-4" /> : null}
              </div>
            ))}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Analyst brief</CardTitle>
            <CardDescription>
              Fast context for the next decision cycle.
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4 md:grid-cols-2">
            <div className="rounded-[24px] border border-border bg-muted/60 p-5">
              <p className="text-xs font-semibold tracking-[0.2em] text-muted-foreground">
                TOP TRIGGER
              </p>
              <p className="mt-4 text-xl font-semibold">
                Velocity rule FDS-204 is driving 36% of high-risk holds.
              </p>
              <p className="mt-3 text-sm leading-7 text-muted-foreground">
                Most events originate from newly created cards with repeated
                attempts under 3 minutes.
              </p>
            </div>
            <div className="rounded-[24px] border border-border bg-muted/60 p-5">
              <p className="text-xs font-semibold tracking-[0.2em] text-muted-foreground">
                INVESTIGATION NOTE
              </p>
              <p className="mt-4 text-xl font-semibold">
                Merchant cluster anomaly remains isolated and reviewable.
              </p>
              <p className="mt-3 text-sm leading-7 text-muted-foreground">
                No broad approval degradation detected across low-risk cohorts
                after the morning rule adjustment.
              </p>
            </div>
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
