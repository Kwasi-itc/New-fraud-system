import { redirect } from "next/navigation";

export default function AuthenticatedIndexPage() {
  redirect("/dashboard");
}
