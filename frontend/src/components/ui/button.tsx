import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-full text-sm font-medium ring-offset-background outline-none disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        default:
          "bg-primary text-primary-foreground shadow-sm hover:-translate-y-0.5 hover:bg-black",
        outline:
          "border border-border bg-white text-foreground hover:-translate-y-0.5 hover:border-foreground hover:bg-muted",
        ghost: "text-foreground hover:bg-muted",
        accent:
          "bg-accent text-accent-foreground shadow-[0_12px_30px_rgba(37,99,235,0.28)] hover:-translate-y-0.5 hover:bg-blue-700",
      },
      size: {
        default: "h-11 px-5",
        sm: "h-9 px-4 text-xs",
        lg: "h-12 px-6",
        icon: "size-11 rounded-2xl",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
);

type ButtonProps = React.ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean;
  };

function Button({
  className,
  variant,
  size,
  asChild = false,
  children,
  ...props
}: ButtonProps) {
  const resolvedClassName = cn(buttonVariants({ variant, size, className }));

  if (asChild) {
    const child = React.Children.only(children);

    if (React.isValidElement<{ className?: string; "data-slot"?: string }>(child)) {
      return React.cloneElement(child, {
        ...child.props,
        ...props,
        "data-slot": "button",
        className: cn(resolvedClassName, child.props.className),
      });
    }
  }

  return (
    <button
      data-slot="button"
      className={resolvedClassName}
      {...props}
    >
      {children}
    </button>
  );
}

export { Button, buttonVariants };
