"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { Search, History, Tag, ArrowLeft } from "lucide-react";
import { SearchHistory } from "./search-history";

export function ScreeningHistoryPage({ scope }: { scope: string }) {
  const router = useRouter();

  return (
    <div className="mx-auto w-full max-w-5xl px-4 py-8 sm:px-6 lg:px-8 space-y-6">
      {/* Navigation & Header */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-slate-200 pb-5">
        <div>
          <div className="flex items-center gap-2 text-sm text-slate-500 mb-1">
            <Link href="/screening-search" className="hover:text-blue-600 flex items-center gap-1">
              <Search className="size-3.5" />
              Screening
            </Link>
            <span>/</span>
            <span className="text-slate-800 font-medium">History</span>
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-slate-900 sm:text-3xl">Screening History</h1>
          <p className="mt-1 text-sm text-slate-600">
            Audit and review your past manual screening queries and potential matches.
          </p>
        </div>

        {/* Tab Links */}
        <div className="flex items-center gap-2">
          <Link
            href="/screening-search"
            className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-3.5 text-sm font-medium text-slate-700 hover:bg-slate-50 transition-colors shadow-2xs"
          >
            <Search className="size-4 text-slate-500" />
            New search
          </Link>
          <Link
            href="/screening-search/lists"
            className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-3.5 text-sm font-medium text-slate-700 hover:bg-slate-50 transition-colors shadow-2xs"
          >
            <Tag className="size-4 text-slate-500" />
            Private lists
          </Link>
        </div>
      </div>

      {/* History Table Container */}
      <div className="rounded-2xl border border-slate-200 bg-white shadow-xs">
        <SearchHistory
          scope={scope}
          disabled={false}
          refreshKey="standalone-page"
          onOpen={id => {
            router.push(`/screening-search?id=${encodeURIComponent(id)}`);
          }}
        />
      </div>
    </div>
  );
}

