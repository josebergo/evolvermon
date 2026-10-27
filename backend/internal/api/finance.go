package api

// M4 财务对接：应收/应付凭证导出（对接用友/金蝶等的外部接口，本系统不自建总账）。

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type FinanceAPI struct{ DB *gorm.DB }

type Voucher struct {
	VoucherNo string  `json:"voucherNo"`
	Date      string  `json:"date"`
	Type      string  `json:"type"` // AR 应收 / AP 应付
	DocNo     string  `json:"docNo"`
	Partner   string  `json:"partner"`
	Amount    float64 `json:"amount"`
	DebitAcc  string  `json:"debitAcc"`
	CreditAcc string  `json:"creditAcc"`
}

// Vouchers 按日期区间导出销售发货（应收）与采购入库（应付）凭证。
func (f *FinanceAPI) Vouchers(c *gin.Context) {
	ac := middleware.GetAuth(c)
	from, to := c.DefaultQuery("from", time.Now().AddDate(0, -1, 0).Format("2006-01-02")),
		c.DefaultQuery("to", time.Now().Format("2006-01-02"))
	var out []Voucher
	seq := 1
	mk := func(t, doc, partner string, amt float64) {
		d, db, cr := t, "1122 应收账款", "6001 主营业务收入"
		if t == "AP" {
			db, cr = "1403 原材料", "2202 应付账款"
		}
		out = append(out, Voucher{
			VoucherNo: time.Now().Format("0601") + "-" + pad(seq), Date: to, Type: d,
			DocNo: doc, Partner: partner, Amount: round2(amt), DebitAcc: db, CreditAcc: cr,
		})
		seq++
	}
	// 应收：本期发货完成的销售单
	var sos []model.SalesOrder
	f.DB.Scopes(data.ScopeFor(ac, true)).Preload("Lines").
		Where("status IN ('partial','done') AND updated_at >= ? AND updated_at < ?::date + interval '1 day'", from, to).Find(&sos)
	var custs []model.Customer
	f.DB.Scopes(data.ScopeFor(ac, false)).Find(&custs)
	custName := map[int64]string{}
	for _, cu := range custs {
		custName[cu.ID] = cu.Name
	}
	for _, so := range sos {
		amt := 0.0
		for _, l := range so.Lines {
			amt += l.ShippedQty * l.Price
		}
		if amt > 0 {
			mk("AR", so.DocNo, custName[so.CustomerID], amt)
		}
	}
	// 应付：本期入库的采购单
	var pos []model.PurchaseOrder
	f.DB.Scopes(data.ScopeFor(ac, true)).Preload("Lines").
		Where("status IN ('receiving','done') AND updated_at >= ? AND updated_at < ?::date + interval '1 day'", from, to).Find(&pos)
	var supps []model.Supplier
	f.DB.Scopes(data.ScopeFor(ac, false)).Find(&supps)
	suppName := map[int64]string{}
	for _, su := range supps {
		suppName[su.ID] = su.Name
	}
	for _, po := range pos {
		amt := 0.0
		for _, l := range po.Lines {
			amt += l.ReceivedQty * l.Price
		}
		if amt > 0 {
			mk("AP", po.DocNo, suppName[po.SupplierID], amt)
		}
	}
	if c.Query("format") == "csv" {
		c.Header("Content-Disposition", "attachment; filename=vouchers.csv")
		c.String(200, "凭证号,日期,类型,业务单号,往来单位,金额,借方科目,贷方科目\n"+
			joinVouchers(out))
		return
	}
	response.OK2(c, out)
}

func joinVouchers(vs []Voucher) string {
	s := ""
	for _, v := range vs {
		s += v.VoucherNo + "," + v.Date + "," + v.Type + "," + v.DocNo + "," + v.Partner + "," +
			ftoa(v.Amount) + "," + v.DebitAcc + "," + v.CreditAcc + "\n"
	}
	return s
}

func pad(n int) string {
	if n < 10 {
		return "000" + i64s(int64(n))
	}
	if n < 100 {
		return "00" + i64s(int64(n))
	}
	return "0" + i64s(int64(n))
}

func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }

func ftoa(f float64) string {
	neg := f < 0
	if neg {
		f = -f
	}
	cents := int64(f*100 + 0.5)
	s := i64s(cents / 100)
	fracS := "00" + i64s(cents%100)
	if neg {
		s = "-" + s
	}
	return s + "." + fracS[len(fracS)-2:]
}
