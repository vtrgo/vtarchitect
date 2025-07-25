import { useMemo } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Progress } from "@/components/ui/progress";
import { ShieldCheck, Cpu, AlertTriangle, PlayCircle, Package, AlertCircle } from "lucide-react";

interface HealthSummaryPanelProps {
  partsPerMinute: number;
  systemTotalParts: number;
  autoModePercentage: number;
  totalFaults: number;
  totalWarnings: number;
  timeRangeLabel: string;
  className?: string;
}

/**
 * A panel that displays a high-level summary of system health, including
 * parts per minute, time in auto mode, and total fault counts.
 */
export function HealthSummaryPanel({
  partsPerMinute,
  systemTotalParts,
  autoModePercentage,
  totalFaults,
  totalWarnings,
  timeRangeLabel,
  className,
}: HealthSummaryPanelProps) {
  const title = `System Health Summary`;

  const autoModeVariant = useMemo((): "success" | "warning" => {
    if (autoModePercentage >= 90) {
      return "success";
    }
    return "warning";
  }, [autoModePercentage]);

  const faultColorClass = totalFaults > 0 ? "text-destructive" : "text-success";
  const warningColorClass = totalWarnings > 0 ? "text-warning" : "text-success";

  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <ShieldCheck className="h-5 w-5 text-primary" />
          <span>{title}</span>
        </CardTitle>
        <p className="text-sm text-muted-foreground pt-1">Overall status for the last {timeRangeLabel}.</p>
      </CardHeader>
      <CardContent className="space-y-6 pt-2">
        <div>
          <div className="flex justify-between items-center">
            <span className="text-sm font-medium text-muted-foreground flex items-center gap-2"><Cpu className="h-4 w-4" />Avg. Parts Per Minute</span>
            <span className="text-lg font-bold">{partsPerMinute.toFixed(1)}</span>
          </div>
        </div>
        <div>
          <div className="flex justify-between items-center">
            <span className="text-sm font-medium text-muted-foreground flex items-center gap-2"><Package className="h-4 w-4" />Total Parts</span>
            <span className="text-lg font-bold">{systemTotalParts.toLocaleString()}</span>
          </div>
        </div>
        <div>
          <div className="flex justify-between items-center mb-1">
            <span className="text-sm font-medium text-muted-foreground flex items-center gap-2"><PlayCircle className="h-4 w-4" />Automatic Mode</span>
            <span className="text-lg font-bold">{autoModePercentage.toFixed(1)}%</span>
          </div>
          <Progress value={autoModePercentage} variant={autoModeVariant} />
        </div>
        <div>
          <div className="flex justify-between items-center">
            <span className="text-sm font-medium text-muted-foreground flex items-center gap-2">
              <AlertTriangle className={`h-4 w-4 ${faultColorClass}`} />Faults
            </span>
            <span className={`text-lg font-bold ${faultColorClass}`}>{totalFaults}</span>
          </div>
        </div>
        <div>
          <div className="flex justify-between items-center">
            <span className="text-sm font-medium text-muted-foreground flex items-center gap-2">
              <AlertCircle className={`h-4 w-4 ${warningColorClass}`} />Warnings
            </span>
            <span className={`text-lg font-bold ${warningColorClass}`}>{totalWarnings}</span>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}