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
import { apiV1Post } from '@/services/client'

import { V1ResourceList } from '@/utils/resource'

import { IResponse } from '../types'
import { NodeSelectorRequirement } from './vcjob'

export interface EstimateItem {
  resources: V1ResourceList
  selectors?: NodeSelectorRequirement[]
}

export interface EstimateResult {
  canRunImmediately: boolean
  estimatedWaitTime: number
  message?: string
}

export interface EstimateWaitTimeReq {
  requests: EstimateItem[]
}

export interface EstimateWaitTimeResp {
  results: EstimateResult[]
}

/**
 * 批量预估作业的等待时间
 */
export const apiEstimateWaitTime = (req: EstimateWaitTimeReq) =>
  apiV1Post<IResponse<EstimateWaitTimeResp>>('estimate', req)
