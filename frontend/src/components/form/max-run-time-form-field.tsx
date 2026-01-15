/**
 * Copyright 2025 RAIDS Lab
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
import { Clock } from 'lucide-react'
import type { FieldPath, FieldValues, UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { FormControl, FormDescription, FormField, FormItem, FormLabel } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

interface MaxRunTimeFormFieldProps<T extends FieldValues> {
  form: UseFormReturn<T>
  name: FieldPath<T>
}

export function MaxRunTimeFormField<T extends FieldValues>({
  form,
  name,
}: MaxRunTimeFormFieldProps<T>) {
  const { t } = useTranslation()

  return (
    <FormField
      control={form.control}
      name={name}
      render={({ field }) => {
        const isLimited = field.value !== null && field.value !== 0 && field.value !== undefined
        const totalSeconds = field.value || 0
        const hours = Math.floor(totalSeconds / 3600)
        const minutes = Math.floor((totalSeconds % 3600) / 60)

        return (
          <FormItem>
            <FormLabel className="flex items-center gap-2">
              <Clock className="size-4" />
              {t('maxRunTime.label')}
            </FormLabel>

            <div className="space-y-4 rounded-lg border p-4">
              <div className="flex items-center justify-between">
                <Label htmlFor="max-run-time-switch" className="text-sm font-medium">
                  {t('maxRunTime.enableLimit')}
                </Label>
                <Switch
                  id="max-run-time-switch"
                  checked={isLimited}
                  onCheckedChange={(checked) => {
                    field.onChange(checked ? 3600 : null)
                  }}
                />
              </div>

              {isLimited && (
                <div className="flex gap-4">
                  <div className="flex-1 space-y-2">
                    <Label className="text-muted-foreground text-xs">{t('maxRunTime.hours')}</Label>
                    <FormControl>
                      <Input
                        type="number"
                        min={0}
                        value={hours}
                        onChange={(e) => {
                          const val = e.target.value === '' ? 0 : parseInt(e.target.value, 10)
                          field.onChange(val * 3600 + minutes * 60)
                        }}
                        placeholder={t('maxRunTime.hoursPlaceholder')}
                      />
                    </FormControl>
                  </div>
                  <div className="flex-1 space-y-2">
                    <Label className="text-muted-foreground text-xs">
                      {t('maxRunTime.minutes')}
                    </Label>
                    <FormControl>
                      <Input
                        type="number"
                        min={0}
                        max={59}
                        value={minutes}
                        onChange={(e) => {
                          const val = e.target.value === '' ? 0 : parseInt(e.target.value, 10)
                          field.onChange(hours * 3600 + val * 60)
                        }}
                        placeholder={t('maxRunTime.minutesPlaceholder')}
                      />
                    </FormControl>
                  </div>
                </div>
              )}
            </div>

            <FormDescription>{t('maxRunTime.description')}</FormDescription>
          </FormItem>
        )
      }}
    />
  )
}
