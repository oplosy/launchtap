import { compactZeros } from "@/amounts";

/**
 * A price with long leading fraction zeros, shown as `0.0₈1699`. Screen readers and copy get the
 * full decimal, because a subscript count is read out as an ordinary digit; hovering shows it too.
 */
export function PriceText({ value }: { value: string }) {
  const compact = compactZeros(value);
  if (compact === value) return <>{value}</>;
  return (
    <>
      <span aria-hidden="true" title={value}>
        {compact}
      </span>
      <span className="sr-only">{value}</span>
    </>
  );
}
