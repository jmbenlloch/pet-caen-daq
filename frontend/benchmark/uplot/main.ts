import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
new uPlot(
  { width: 1200, height: 600, series: [{}, {}] },
  [new Float64Array(), new Float64Array()],
  document.querySelector('#chart')!,
)
