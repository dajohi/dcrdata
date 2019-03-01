// shared functions for related to charts

export function barChartPlotter (e) {
  plotChart(e, fitWidth(e.points));
}

function fitWidth (points) {
  let minSep = Infinity;
  for (let i = 1; i < points.length; i++) {
    const sep = points[i].canvasx - points[i - 1].canvasx;
    if (sep < minSep) minSep = sep;
  }
  return Math.floor(2.0 / 3 * minSep);
}

export function sizedBarPlotter (binSize) {
  return (e) => {
    const canvasBin = e.dygraph.toDomXCoord(binSize) - e.dygraph.toDomXCoord(0);
    plotChart(e, Math.floor(2.0 / 3 * canvasBin));
  };
}

function plotChart (e, barWidth) {
  const ctx = e.drawingContext;
  const yBottom = e.dygraph.toDomYCoord(0);

  ctx.fillStyle = e.color;

  e.points.map((p) => {
    const x = p.canvasx - barWidth / 2;
    const height = yBottom - p.canvasy;
    ctx.fillRect(x, p.canvasy, barWidth, height);
    ctx.strokeRect(x, p.canvasy, barWidth, height);
  });
}

export function padPoints (pts, binSize, sustain) {
  let pad = binSize / 2.0;
  const lastPt = pts[pts.length - 1];
  const firstPt = pts[0];
  const frontStamp = firstPt[0].getTime();
  const backStamp = lastPt[0].getTime();
  const duration = backStamp - frontStamp;
  if (duration < binSize) {
    pad = Math.max(pad, (binSize - duration) / 2.0);
  }
  const front = [new Date(frontStamp - pad)];
  const back = [new Date(backStamp + pad)];
  for (let i = 1; i < firstPt.length; i++) {
    front.push(0);
    back.push(sustain ? lastPt[i] : 0);
  }
  pts.unshift(front);
  pts.push(back);
}
