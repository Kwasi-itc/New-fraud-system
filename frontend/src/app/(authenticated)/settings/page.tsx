import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export default function SettingsPage() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Settings</CardTitle>
        <CardDescription>
          Tenant controls, access policy, and notification routing.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid gap-4 md:grid-cols-2">
          <div className="rounded-[24px] bg-muted p-5">
            <p className="text-sm font-medium">Authentication</p>
            <p className="mt-2 text-sm leading-7 text-muted-foreground">
              SSO, MFA, and analyst role mapping will live in this section.
            </p>
          </div>
          <div className="rounded-[24px] bg-muted p-5">
            <p className="text-sm font-medium">Notifications</p>
            <p className="mt-2 text-sm leading-7 text-muted-foreground">
              Escalation channels, pager integrations, and SLA reminders.
            </p>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
