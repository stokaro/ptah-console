import type { ButtonHTMLAttributes } from "react"

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "secondary" | "attention"
}

const variants = {
  primary:
    "border-0 bg-linear-135 from-pri to-[color-mix(in_oklab,var(--pri)_62%,var(--teal))] px-[18px] font-semibold text-onpri",
  secondary: "border border-line bg-card px-4 font-medium text-fg",
  attention: "border border-violet bg-card px-4 font-medium text-violet",
}

/** A button at the design's touch size: 44 pixels high. */
export function Button({
  variant = "secondary",
  className = "",
  type = "button",
  ...props
}: ButtonProps) {
  return (
    <button
      type={type}
      className={`inline-flex min-h-11 cursor-pointer items-center justify-center gap-2 rounded-xl text-sm whitespace-nowrap disabled:cursor-not-allowed disabled:opacity-45 ${variants[variant]} ${className}`}
      {...props}
    />
  )
}
