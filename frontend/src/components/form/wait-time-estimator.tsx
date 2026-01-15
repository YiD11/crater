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
import { useQuery } from '@tanstack/react-query'
import { AlertCircle, CheckCircle2, Clock, Loader2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { FieldPath, FieldValues, UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

import { apiEstimateWaitTime } from '@/services/api/estimator'

import { ResourceSchema, convertToResourceList } from '@/utils/form'

interface WaitTimeEstimatorProps<T extends FieldValues> {
  form: UseFormReturn<T>
  resourcePath: {
    cpu: FieldPath<T>
    memory: FieldPath<T>
    gpuCount: FieldPath<T>
    gpuModel: FieldPath<T>
  }
  selectorsPath?: FieldPath<T>
}

export function WaitTimeEstimator<T extends FieldValues>({
  form,
  resourcePath,
  selectorsPath,
}: WaitTimeEstimatorProps<T>) {
  const { t } = useTranslation()

  const cpu = form.watch(resourcePath.cpu)
  const memory = form.watch(resourcePath.memory)
  const gpuCount = form.watch(resourcePath.gpuCount)
  const gpuModel = form.watch(resourcePath.gpuModel)
  const selectors = selectorsPath ? form.watch(selectorsPath) : undefined

  const [debouncedResource, setDebouncedResource] = useState<ResourceSchema | null>(null)

  const resourceKey = useMemo(() => {
    return `${cpu}-${memory}-${gpuCount}-${gpuModel}-${JSON.stringify(selectors)}`
  }, [cpu, memory, gpuCount, gpuModel, selectors])

  useEffect(() => {
    const timer = setTimeout(() => {
      if (cpu > 0 || memory > 0 || gpuCount > 0) {
        setDebouncedResource({
          cpu: cpu || 0,
          memory: memory || 0,
          gpu: {
            count: gpuCount || 0,
            model: gpuModel,
          },
          network: { enabled: false },
          vgpu: { enabled: false },
        })
      }
    }, 800)

    return () => clearTimeout(timer)
  }, [resourceKey, cpu, memory, gpuCount, gpuModel])

  const { data, isLoading, isError } = useQuery({
    queryKey: ['estimateWaitTime', debouncedResource, selectors],
    queryFn: async () => {
      if (!debouncedResource) return null
      const resources = convertToResourceList(debouncedResource)
      const response = await apiEstimateWaitTime({ requests: [{ resources, selectors }] })
      return response.data.results[0]
    },
    enabled: !!debouncedResource && (cpu > 0 || memory > 0),
    staleTime: 5000,
    refetchInterval: 10000,
  })

  if (!debouncedResource || (cpu <= 0 && memory <= 0 && gpuCount <= 0)) {
    return null
  }

  if (isLoading) {
    return (
      <Alert className="border-muted bg-muted/50">
        <Loader2 className="size-4 animate-spin" />
        <AlertTitle>{t('waitTimeEstimator.loading')}</AlertTitle>
      </Alert>
    )
  }

  if (isError) {
    return (
      <Alert variant="destructive">
        <AlertCircle className="size-4" />
        <AlertTitle>{t('waitTimeEstimator.error')}</AlertTitle>
      </Alert>
    )
  }

  if (!data) {
    return null
  }

  if (data.canRunImmediately) {
    return (
      <Alert className="border-green-500/50 bg-green-500/10 text-green-700 dark:text-green-400">
        <CheckCircle2 className="size-4" />
        <AlertTitle>{t('waitTimeEstimator.canRunImmediately')}</AlertTitle>
      </Alert>
    )
  }

  if (data.estimatedWaitTime < 0) {
    return (
      <Alert variant="destructive">
        <AlertCircle className="size-4" />
        <AlertTitle>{t('waitTimeEstimator.resourceInsufficient')}</AlertTitle>
        {data.message && <AlertDescription>{data.message}</AlertDescription>}
      </Alert>
    )
  }

  const estimatedWaitSec = Math.ceil(data.estimatedWaitTime / 1_000_000_000)
  const waitMinutes = Math.ceil(estimatedWaitSec / 60)
  const displayText =
    waitMinutes >= 1
      ? t('waitTimeEstimator.estimatedWait', { minutes: waitMinutes })
      : t('waitTimeEstimator.estimatedWaitSeconds', { seconds: estimatedWaitSec })

  return (
    <Alert className="border-orange-500/50 bg-orange-500/10 text-orange-700 dark:text-orange-400">
      <Clock className="size-4" />
      <AlertTitle>{displayText}</AlertTitle>
      {data.message && <AlertDescription>{data.message}</AlertDescription>}
    </Alert>
  )
}
