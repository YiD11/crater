import { Layers3Icon, Loader2Icon, SaveIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

interface SchedulerExtenderSettingsProps {
  queueQuotaEnabled: boolean
  schedulerExtenderEnabled: boolean
  isPending: boolean
  waitingToleranceSeconds: string
  onQueueQuotaEnabledChange: (enabled: boolean) => void
  onSchedulerExtenderEnabledChange: (enabled: boolean) => void
  onWaitingToleranceSecondsChange: (value: string) => void
  onSubmit: () => void
}

export function SchedulerExtenderSettings({
  queueQuotaEnabled,
  schedulerExtenderEnabled,
  isPending,
  waitingToleranceSeconds,
  onQueueQuotaEnabledChange,
  onSchedulerExtenderEnabledChange,
  onWaitingToleranceSecondsChange,
  onSubmit,
}: SchedulerExtenderSettingsProps) {
  const { t } = useTranslation()
  const hasEnabledFeature = queueQuotaEnabled || schedulerExtenderEnabled

  return (
    <>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Layers3Icon
              className={
                hasEnabledFeature ? 'h-5 w-5 text-blue-500' : 'text-muted-foreground h-5 w-5'
              }
            />
            <CardTitle>{t('systemConfig.schedulerExtender.title')}</CardTitle>
          </div>
        </div>
        <CardDescription>{t('systemConfig.schedulerExtender.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-4 md:grid-cols-2">
          <div className="flex items-center justify-between rounded-lg border p-4 shadow-sm">
            <div>
              <Label className="text-base">
                {t('systemConfig.schedulerExtender.queueQuotaSwitchLabel')}
              </Label>
            </div>
            <div className="flex items-center gap-2">
              {isPending && <Loader2Icon className="text-muted-foreground h-4 w-4 animate-spin" />}
              <Switch
                checked={queueQuotaEnabled}
                onCheckedChange={onQueueQuotaEnabledChange}
                disabled={isPending}
              />
            </div>
          </div>

          <div className="flex items-center justify-between rounded-lg border p-4 shadow-sm">
            <div>
              <Label className="text-base">
                {t('systemConfig.schedulerExtender.extenderSwitchLabel')}
              </Label>
            </div>
            <div className="flex items-center gap-2">
              {isPending && <Loader2Icon className="text-muted-foreground h-4 w-4 animate-spin" />}
              <Switch
                checked={schedulerExtenderEnabled}
                onCheckedChange={onSchedulerExtenderEnabledChange}
                disabled={isPending}
              />
            </div>
          </div>
        </div>

        <div className="space-y-2">
          <Label htmlFor="scheduler-extender-waiting-tolerance">
            {t('systemConfig.schedulerExtender.waitingToleranceLabel')}
          </Label>
          <Input
            id="scheduler-extender-waiting-tolerance"
            type="number"
            min={1}
            value={waitingToleranceSeconds}
            onChange={(event) => onWaitingToleranceSecondsChange(event.target.value)}
            disabled={isPending}
            placeholder={t('systemConfig.schedulerExtender.waitingTolerancePlaceholder')}
          />
        </div>
      </CardContent>
      <CardFooter className="bg-muted/10 px-6 py-4">
        <Button type="button" onClick={onSubmit} disabled={isPending}>
          <SaveIcon className="mr-2 h-4 w-4" />
          {t('systemConfig.schedulerExtender.save')}
        </Button>
      </CardFooter>
    </>
  )
}
