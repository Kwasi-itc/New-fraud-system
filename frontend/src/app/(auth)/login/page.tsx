import Link from "next/link";
import { ArrowRight } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";

export default function LoginPage() {
  return (
    <Card className="w-full rounded-[26px] border-[#d8e1f0] bg-white/96 shadow-[0_20px_60px_rgba(15,23,42,0.08)] backdrop-blur">
      <CardHeader className="gap-2.5 px-6 pt-6 pb-4 sm:px-7 sm:pt-7">
        <div className="space-y-1">
          <CardTitle className="text-[1.7rem] font-semibold tracking-tight text-slate-900 sm:text-[1.8rem]">
            Sign In
          </CardTitle>
          <CardDescription className="text-[0.98rem] leading-7 text-slate-600">
            Enter your credentials to access the platform
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 px-6 pb-6 sm:px-7 sm:pb-7">
        <form className="space-y-4.5">
          <div className="space-y-2">
            <label className="text-sm font-semibold text-slate-900" htmlFor="email">
              Email
            </label>
            <Input
              id="email"
              type="email"
              placeholder="user@itc.com"
              className="h-13 rounded-2xl border-[#e9eef6] bg-[#f4f7fb] px-4 text-[15px] text-slate-700 placeholder:text-slate-500 focus:border-accent focus:bg-white focus:ring-[3px] focus:ring-blue-100"
            />
          </div>
          <div className="space-y-2">
            <div className="flex items-center justify-between gap-4">
              <label className="text-sm font-semibold text-slate-900" htmlFor="password">
                Password
              </label>
              <Link
                href="#"
                className="text-sm font-medium text-slate-500 hover:text-slate-900"
              >
                Forgot password?
              </Link>
            </div>
            <Input
              id="password"
              type="password"
              placeholder="Enter password"
              className="h-13 rounded-2xl border-[#e9eef6] bg-[#f4f7fb] px-4 text-[15px] text-slate-700 placeholder:text-slate-500 focus:border-accent focus:bg-white focus:ring-[3px] focus:ring-blue-100"
            />
          </div>
          <Button
            className="mt-2 h-13 w-full rounded-2xl bg-[#184a8b] text-[15px] font-semibold text-white shadow-none hover:bg-[#123d73]"
            size="lg"
            type="submit"
          >
            Sign in
            <ArrowRight className="size-4" />
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
