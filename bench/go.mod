module github.com/prathump/stundb/bench

go 1.26

replace github.com/prathump/stundb => ../

require (
	github.com/google/btree v1.1.3
	github.com/prathump/stundb v0.0.0-00010101000000-000000000000
	github.com/puzpuzpuz/xsync/v4 v4.5.0
	github.com/tidwall/btree v1.8.2
	github.com/zhangyunhao116/skipmap v0.10.1
)

require github.com/zhangyunhao116/fastrand v0.3.0 // indirect
