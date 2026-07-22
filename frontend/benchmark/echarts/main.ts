import * as echarts from 'echarts/core'
import { LineChart, HeatmapChart } from 'echarts/charts'
import {
  GridComponent,
  LegendComponent,
  TooltipComponent,
  DataZoomComponent,
  VisualMapComponent,
} from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
echarts.use([
  LineChart,
  HeatmapChart,
  GridComponent,
  LegendComponent,
  TooltipComponent,
  DataZoomComponent,
  VisualMapComponent,
  CanvasRenderer,
])
echarts.init(document.querySelector('#chart')!).setOption({ xAxis: {}, yAxis: {}, series: [] })
