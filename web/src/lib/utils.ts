import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * Returns the URL unchanged if it parses as http/https, or null otherwise. Values like
 * suggested_method_url come from an externally-sourced, lower-trust dataset (the
 * enrichment corpus, not the provider's own workbooks); rendering an unvalidated href
 * risks a javascript: URL XSS if that source ever carried one. Server components have
 * no `window`, so this parses without a base and only accepts absolute http/https URLs.
 */
export function safeHref(url: string): string | null {
  try {
    const parsed = new URL(url)
    return parsed.protocol === "http:" || parsed.protocol === "https:" ? url : null
  } catch {
    return null
  }
}
