import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

const cases = [
  ["FD-3182", "Account takeover", "High", "Awaiting callback"],
  ["FD-3178", "Card testing", "Critical", "Evidence review"],
  ["FD-3172", "Merchant collusion", "Medium", "Analyst assigned"],
];

export default function CasesPage() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Cases</CardTitle>
        <CardDescription>
          Active investigations prioritized for the current shift.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {cases.map(([id, type, severity, stage]) => (
          <div
            key={id}
            className="grid gap-3 rounded-[24px] border border-border p-5 md:grid-cols-[120px_1fr_120px_180px]"
          >
            <p className="font-semibold">{id}</p>
            <p>{type}</p>
            <Badge
              variant={
                severity === "Critical"
                  ? "warning"
                  : severity === "High"
                    ? "accent"
                    : "neutral"
              }
            >
              {severity}
            </Badge>
            <p className="text-sm text-muted-foreground">{stage}</p>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}
