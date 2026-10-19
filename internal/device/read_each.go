package device

import (
	"context"
	"fmt"
	"time"
)

// ReadEach sends one address per read and reports each completed attempt.
// A rejected item does not stop the scan; transport errors and cancellation do.
func ReadEach(ctx context.Context, driver Driver, addresses []Address, progress func(ReadResult)) ([]ReadResult, error) {
	if len(addresses) < 1 || len(addresses) > 1024 {
		return nil, fmt.Errorf("read requires 1..1024 addresses")
	}
	total := 0
	for _, a := range addresses {
		if err := a.Validate(); err != nil {
			return nil, err
		}
		if a.Symbol != "" {
			return nil, fmt.Errorf("sequential read requires absolute addresses")
		}
		total += a.ByteLength()
		if total > 16<<20 {
			return nil, fmt.Errorf("read exceeds 16 MiB result limit")
		}
	}
	results := make([]ReadResult, len(addresses))
	for i, a := range addresses {
		results[i] = ReadResult{Address: a, Quality: "UNKNOWN", Error: "未读取"}
	}
	unread := func(start int, err error) ([]ReadResult, error) {
		for i := start; i < len(results); i++ {
			results[i].Error = "未读取：" + err.Error()
		}
		return results, err
	}
	for i, a := range addresses {
		if err := ctx.Err(); err != nil {
			return unread(i, err)
		}
		got, err := driver.Read(ctx, []Address{a})
		if len(got) == 1 {
			results[i] = got[0]
		} else {
			if err == nil {
				err = fmt.Errorf("single-point read returned %d results", len(got))
			}
			results[i].Time = time.Now().UTC()
			results[i].Error = err.Error()
		}
		if progress != nil {
			progress(results[i])
		}
		if err != nil {
			return unread(i+1, err)
		}
	}
	return results, nil
}
