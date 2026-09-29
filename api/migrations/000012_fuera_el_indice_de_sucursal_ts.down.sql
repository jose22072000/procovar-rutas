-- Volver a crearlo, por si alguna vez hiciera falta consultar por sucursal y fecha.
-- Son 1.604 MB y un buen rato de construcción.
CREATE INDEX IF NOT EXISTS track_point_sucursal_ts_idx ON track_point (sucursal_id, ts);
