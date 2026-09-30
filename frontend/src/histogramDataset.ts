import type { DeepReadonly } from 'vue'
import type { HistogramDataset } from './gen/pet/caen/daq/v1/system_pb'

// Board bins arrive already summed. 64 uint32 bins fit exactly in JS numbers.
export function histogramBins(dataset: DeepReadonly<HistogramDataset>): readonly number[] {
  return dataset.boardSum ? dataset.boardSumBins.map(Number) : dataset.bins
}
export function histogramLabel(dataset: DeepReadonly<HistogramDataset>) {
  return `Chain ${dataset.chain} · Node ${dataset.node} · ${dataset.boardSum ? 'Board sum (all 64 channels)' : `CH ${dataset.channel}`}`
}
