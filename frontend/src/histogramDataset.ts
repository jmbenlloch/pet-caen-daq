import type { DeepReadonly } from 'vue'
import type { HistogramDataset } from './gen/pet/caen/daq/v1/system_pb'

// Board bins arrive from the server. Up to 64 uint32 counts fit exactly in a JS number.
export function histogramBins(dataset: DeepReadonly<HistogramDataset>): readonly number[] {
  return dataset.boardSum ? dataset.boardSumBins.map(Number) : dataset.bins
}
export function histogramLabel(
  dataset: DeepReadonly<HistogramDataset>,
  boardHistogramLabel = 'Board sum (all 64 channels)',
) {
  return `Chain ${dataset.chain} · Node ${dataset.node} · ${dataset.boardSum ? boardHistogramLabel : `CH ${dataset.channel}`}`
}
