ALTER TABLE live_services DROP CONSTRAINT live_services_kind_check;
ALTER TABLE live_services ADD CONSTRAINT live_services_kind_check CHECK(kind IN ('shadowsocks','frps','frpc','vless','hysteria2','tuic','trojan','snell'));
ALTER TABLE live_services ADD COLUMN engine text NOT NULL DEFAULT 'sing-box' CHECK(engine IN ('sing-box','xray'));
