# -*- coding: utf-8 -*-
import io, re

p = 'internal/wms/posting/writeback_api.go'
s = io.open(p, encoding='utf-8', newline='').read()
nl = '\r\n' if '\r\n' in s[:3000] else '\n'

# 1) WBLine 加 MaterialID
old_type = nl.join([
 '// WBLine 回写行（sourceLineId 由 Find*Line 语义简化为按 material+qty 匹配，服务端自行定位）。',
 'type WBLine struct {',
 '\tLineID int64   `json:"lineId"`',
 '\tQty    float64 `json:"qty"`',
 '}',
])
new_type = nl.join([
 '// WBLine 回写行（三库：携带 materialId，端点进程无 wms 表无法回查行）。',
 'type WBLine struct {',
 '\tLineID     int64   `json:"lineId"`',
 '\tQty        float64 `json:"qty"`',
 '\tMaterialID int64   `json:"materialId"`',
 '}',
])
assert old_type in s, 'WBLine'
s = s.replace(old_type, new_type, 1)

# 2) WriteBackLocal 行携带 MaterialID + order.Lines
old_ll = nl.join([
 '\tlines := make([]PostLine, 0, len(req.Lines))',
 '\tfor _, l := range req.Lines {',
 '\t\tlines = append(lines, PostLine{LineID: l.LineID, Qty: l.Qty})',
 '\t}',
])
new_ll = nl.join([
 '\tlines := make([]PostLine, 0, len(req.Lines))',
 '\tfor _, l := range req.Lines {',
 '\t\tlines = append(lines, PostLine{LineID: l.LineID, Qty: l.Qty, MaterialID: l.MaterialID})',
 '\t}',
])
assert old_ll in s, 'lines'
s = s.replace(old_ll, new_ll, 1)

# 3) wbReq 携带 MaterialID（需 order.Lines 的行物料——PostLinesTx 里 lines 无物料；改从 line 查过账行时带上）
s = s.replace('wbl = append(wbl, WBLine{LineID: l.LineID, Qty: l.Qty})',
              'wbl = append(wbl, WBLine{LineID: l.LineID, Qty: l.Qty, MaterialID: l.MaterialID})')

# 4) wbOrderLines 辅助 + order.Lines 构造
anchor = 'order := model.StockOrder{ID: req.OrderID, DocNo: req.DocNo, SourceType: req.Source, SourceDocID: req.SourceDocID}'
assert anchor in s, 'order anchor'
s = s.replace(anchor, 'order := model.StockOrder{ID: req.OrderID, DocNo: req.DocNo, SourceType: req.Source, SourceDocID: req.SourceDocID, Lines: wbOrderLines(req.Lines)}', 1)
s = s.rstrip() + nl + nl.join([
 '',
 'func wbOrderLines(ls []WBLine) []model.StockOrderLine {',
 '\tout := make([]model.StockOrderLine, 0, len(ls))',
 '\tfor _, l := range ls {',
 '\t\tout = append(out, model.StockOrderLine{ID: l.LineID, MaterialID: l.MaterialID, Qty: l.Qty})',
 '\t}',
 '\treturn out',
 '}',
 ''])
io.open(p, 'w', encoding='utf-8', newline='').write(s)
print('writeback_api done')

# 5) posting.go：PostLine 加 MaterialID；FindSOLine/POLine 不查 wms 行；PostLinesTx 构造 lines 时带物料
p = 'internal/wms/posting/posting.go'
s = io.open(p, encoding='utf-8', newline='').read()
nl2 = '\r\n' if '\r\n' in s[:3000] else '\n'
old_pl = nl2.join([
 'type PostLine struct {',
 '\tLineID     int64   `json:"lineId"`',
 '\tQty        float64 `json:"qty"`',
 '\tBatchNo    string  `json:"batchNo"`',
 '\tLocationID *int64  `json:"locationId"`',
 '}',
])
new_pl = nl2.join([
 'type PostLine struct {',
 '\tLineID     int64   `json:"lineId"`',
 '\tQty        float64 `json:"qty"`',
 '\tBatchNo    string  `json:"batchNo"`',
 '\tLocationID *int64  `json:"locationId"`',
 '\tMaterialID int64   `json:"materialId"` // 三库回写携带',
 '}',
])
if old_pl in s:
    s = s.replace(old_pl, new_pl, 1)

old_f = nl2.join([
 'func FindSOLine(tx *gorm.DB, order model.StockOrder, pl PostLine) int64 {',
 '\tvar l model.StockOrderLine',
 '\ttx.First(&l, pl.LineID)',
 '\tvar sol model.SalesOrderLine',
])
new_f = nl2.join([
 'func FindSOLine(tx *gorm.DB, order model.StockOrder, pl PostLine) int64 {',
 '\tl := model.StockOrderLine{ID: pl.LineID, MaterialID: pl.MaterialID}',
 '\tvar sol model.SalesOrderLine',
])
assert old_f in s, 'FindSOLine'
s = s.replace(old_f, new_f, 1)

old_p = nl2.join([
 'func FindPOLine(tx *gorm.DB, order model.StockOrder, pl PostLine) int64 {',
 '\tvar l model.StockOrderLine',
 '\ttx.First(&l, pl.LineID)',
 '\tvar pol model.PurchaseOrderLine',
])
new_p = nl2.join([
 'func FindPOLine(tx *gorm.DB, order model.StockOrder, pl PostLine) int64 {',
 '\tl := model.StockOrderLine{ID: pl.LineID, MaterialID: pl.MaterialID}',
 '\tvar pol model.PurchaseOrderLine',
])
assert old_p in s, 'FindPOLine'
s = s.replace(old_p, new_p, 1)

# PostLinesTx 里循环行已加载 line（model.StockOrderLine 含 MaterialID）——把 pl.MaterialID 填上
m = re.search(r'(\t\t\tpl := )', s)
if m:
    # 找到 for _, pl := range lines 处构造——lines 元素已是 PostLine（API 层构造无 MaterialID）。API 层 PostLines handler 组 req.Lines 只有 lineId/qty——在 PostLinesTx 开头 enrich
    old_loop = nl2.join([
     '\treturn db.Transaction(func(tx *gorm.DB) error {',
     '\t\tfor _, pl := range lines {',
    ])
    new_loop = nl2.join([
     '\t// 行物料回填（Find*Line 三库直配用）',
     '\tfor i := range lines {',
     '\t\tvar line model.StockOrderLine',
     '\t\tif err := db.Where("stock_order_id=?", order.ID).First(&line, lines[i].LineID).Error; err == nil {',
     '\t\t\tlines[i].MaterialID = line.MaterialID',
     '\t\t}',
     '\t}',
     '\treturn db.Transaction(func(tx *gorm.DB) error {',
     '\t\tfor _, pl := range lines {',
    ])
    if old_loop in s:
        s = s.replace(old_loop, new_loop, 1)
        print('PostLinesTx enrich')
io.open(p, 'w', encoding='utf-8', newline='').write(s)
print('posting done')
