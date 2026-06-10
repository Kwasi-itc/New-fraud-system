import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export default function DetectionPage() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Detection</CardTitle>
        <CardDescription>
          A clean surface for model health, drift, live alerting, and rule pressure.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid gap-4 md:grid-cols-3">
          {["Velocity", "Device trust", "Chargeback predictor"].map((item) => (
            <div key={item} className="rounded-[24px] bg-muted p-5">
              <p className="text-sm text-muted-foreground">{item}</p>
              <p className="mt-4 text-3xl font-semibold">Stable</p>
              <p className="mt-2 text-sm leading-6 text-muted-foreground">
                No severe drift detected in the last 6 hours.
              </p>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}
