-- The Drive route folders, taken from the tablets.procovar parent account on the
-- 15th of August 2026.
--
-- Each shared folder IS a seller's GPS profile, so its NAME is the hint that
-- resolves whose the files are — they only carry the date (YYYYMMDD.gpx).
--
-- The type is MIXTA because the seller is not known yet: it gets matched once from
-- the panel's inbox and from then on resolves on its own.
--
-- Why this is a migration and not a script somebody runs: without these rows the
-- ingest rejects every file n8n pushes ("the folder is not registered"), so the
-- system does not work at all until they exist. Something the application needs in
-- order to run is not a seed, it is part of the deployment. It is idempotent
-- (ON CONFLICT DO NOTHING) and does not touch what has been added from the screen.
--
-- The trailing number is how many .gpx each folder had on the day of the dump.
--
-- They go in with no branch and no account on purpose: everything comes through the
-- parent account, so who OWNS each shared folder is what says which branch it
-- belongs to, and that arrives with the file (Drive's `owners`). The first push
-- fills it in.

INSERT INTO drive_source (id, nombre, folder_id, tipo, credencial) VALUES
    (md5('1lO4WmKdE0-lyhI2oIBwD_OiDPuv0Z4y-'), 'JEAN MICHEL', '1lO4WmKdE0-lyhI2oIBwD_OiDPuv0Z4y-', 'MIXTA', 'principal'),  -- 118
    (md5('1bHdcqVtUYuTJmtjZQBS9mAIDX7u2fSoN'), 'DEYANIRA', '1bHdcqVtUYuTJmtjZQBS9mAIDX7u2fSoN', 'MIXTA', 'principal'),  -- 112
    (md5('10cMhWFAwWqnmtMTg6Kh1WHJYzjD0IslG'), 'STGArys', '10cMhWFAwWqnmtMTg6Kh1WHJYzjD0IslG', 'MIXTA', 'principal'),  -- 112
    (md5('18ozafkq92SqHcdrdXofNC4BxatT3wnbi'), 'STGDiango', '18ozafkq92SqHcdrdXofNC4BxatT3wnbi', 'MIXTA', 'principal'),  -- 111
    (md5('1Dxe_Whkfc1OdnFRdns78efPLf2B24hjR'), 'GEORLI', '1Dxe_Whkfc1OdnFRdns78efPLf2B24hjR', 'MIXTA', 'principal'),  -- 108
    (md5('1nsnJpRE-B6aQsCLwl38gimI_y8rDpdPG'), 'MAYLEN', '1nsnJpRE-B6aQsCLwl38gimI_y8rDpdPG', 'MIXTA', 'principal'),  -- 103
    (md5('1SHvLzflZUKBAarwi5CiEicL6TEJqzPDe'), 'STGGari', '1SHvLzflZUKBAarwi5CiEicL6TEJqzPDe', 'MIXTA', 'principal'),  -- 97
    (md5('1rtcHGI3ENFebaMo3gMxAt4mfNm_1UVhk'), 'STGDayla', '1rtcHGI3ENFebaMo3gMxAt4mfNm_1UVhk', 'MIXTA', 'principal'),  -- 96
    (md5('19EDi3VDU0pGdTPvPizGFZ8UZy0aWLxQD'), 'STGTadyslai', '19EDi3VDU0pGdTPvPizGFZ8UZy0aWLxQD', 'MIXTA', 'principal'),  -- 94
    (md5('1N_TVXDtoqmH__NKio81TATxPzdm9HSOG'), 'ANDY', '1N_TVXDtoqmH__NKio81TATxPzdm9HSOG', 'MIXTA', 'principal'),  -- 92
    (md5('1tk0EA1WauTQ1rDYBrGPwA2N2DmPc0QAw'), 'STGClaudia', '1tk0EA1WauTQ1rDYBrGPwA2N2DmPc0QAw', 'MIXTA', 'principal'),  -- 92
    (md5('1EY03JcRkSpmA04bc7kQcw41F05qhaoc8'), 'ALFREDO', '1EY03JcRkSpmA04bc7kQcw41F05qhaoc8', 'MIXTA', 'principal'),  -- 88
    (md5('12-LQ_HzSudwHG8otv2_p0PM4aavhk7ww'), 'ALEXANDER', '12-LQ_HzSudwHG8otv2_p0PM4aavhk7ww', 'MIXTA', 'principal'),  -- 62
    (md5('1A3pHEyZLXUxAOsxQdKp7ffCy0dUL8nu_'), 'STGGlenda', '1A3pHEyZLXUxAOsxQdKp7ffCy0dUL8nu_', 'MIXTA', 'principal'),  -- 47
    (md5('13oNlvAo9cdyB_kkCt4jygXd_RhAeKZpJ'), 'GPS Fidel Alberto', '13oNlvAo9cdyB_kkCt4jygXd_RhAeKZpJ', 'MIXTA', 'principal'),  -- 40
    (md5('1MyFeLxPhlQtAdNf_woZr1aAmNmYV6Nue'), 'GTMArlen', '1MyFeLxPhlQtAdNf_woZr1aAmNmYV6Nue', 'MIXTA', 'principal'),  -- 32
    (md5('12F9WXry6V4hlX5BPPMXfWQ6mBq4JpM5O'), 'GTMYamisleidy', '12F9WXry6V4hlX5BPPMXfWQ6mBq4JpM5O', 'MIXTA', 'principal'),  -- 32
    (md5('1e22GFiyW3baG5XPrl3hePo7Cx-HIzFGE'), 'GTMRolando', '1e22GFiyW3baG5XPrl3hePo7Cx-HIzFGE', 'MIXTA', 'principal'),  -- 31
    (md5('1X0dEszb5v8ANUEs42Zp7e8yKBRZgooJ6'), 'GTMLaura', '1X0dEszb5v8ANUEs42Zp7e8yKBRZgooJ6', 'MIXTA', 'principal'),  -- 30
    (md5('1TH-b3G2jHYqpDYBzof4jn_39sN0D6ul3'), 'GTMEryslandy', '1TH-b3G2jHYqpDYBzof4jn_39sN0D6ul3', 'MIXTA', 'principal'),  -- 29
    (md5('1UglWQpRBxbDP39MBB-JgU7vvqscwelHk'), 'Supervisora BAY', '1UglWQpRBxbDP39MBB-JgU7vvqscwelHk', 'MIXTA', 'principal'),  -- 26
    (md5('11ea34eWO0v7k30O3MLJeKT9yXezruE2Z'), 'CAM2', '11ea34eWO0v7k30O3MLJeKT9yXezruE2Z', 'MIXTA', 'principal'),  -- 25
    (md5('1mZdUSdLq54kbyz7jZvnFAT19dkv03GkX'), 'GPS Javier de Jesus', '1mZdUSdLq54kbyz7jZvnFAT19dkv03GkX', 'MIXTA', 'principal'),  -- 21
    (md5('1hfEG8z8P9DpOcWARfTDqWJ-mhicDkaqe'), 'GPSXenia', '1hfEG8z8P9DpOcWARfTDqWJ-mhicDkaqe', 'MIXTA', 'principal'),  -- 21
    (md5('1V1ng6TqSNWqoDRwHAEKhdQfu3MtWBWFs'), 'GPS leisy', '1V1ng6TqSNWqoDRwHAEKhdQfu3MtWBWFs', 'MIXTA', 'principal'),  -- 20
    (md5('1JuWRIXwfID8wRKYt5h_vL-KL_f-CDYlp'), 'GPS Raydel', '1JuWRIXwfID8wRKYt5h_vL-KL_f-CDYlp', 'MIXTA', 'principal'),  -- 19
    (md5('1Qjqm8PEmGm2TPhaBtruPjKkduWumDHtP'), 'Niurbelis Sánchez', '1Qjqm8PEmGm2TPhaBtruPjKkduWumDHtP', 'MIXTA', 'principal'),  -- 19
    (md5('1HnTQPbACgxUXkpKM2rOO9w6X1sogEPMf'), 'Yaimara', '1HnTQPbACgxUXkpKM2rOO9w6X1sogEPMf', 'MIXTA', 'principal'),  -- 19
    (md5('15MmX7OQTGMfHZFDStcp08j3Dk0r3qojB'), 'GPSRigoberto', '15MmX7OQTGMfHZFDStcp08j3Dk0r3qojB', 'MIXTA', 'principal'),  -- 16
    (md5('1COS2LB_1PH7QZNpxCspm2XgfiSVkWUEn'), 'AVILIO', '1COS2LB_1PH7QZNpxCspm2XgfiSVkWUEn', 'MIXTA', 'principal'),  -- 14
    (md5('1lJVCiH2u83CacRtVVC1blFM0HCC8UMjL'), 'Alendy Torres GPS', '1lJVCiH2u83CacRtVVC1blFM0HCC8UMjL', 'MIXTA', 'principal'),  -- 14
    (md5('1OT8EgO4j046AyH-LXyzTVauCorlK1bmH'), 'GPS luis', '1OT8EgO4j046AyH-LXyzTVauCorlK1bmH', 'MIXTA', 'principal'),  -- 14
    (md5('15-YIXu7jME0jEy6KWgo4iqGnuMgRMKvf'), 'GPS Yankelly', '15-YIXu7jME0jEy6KWgo4iqGnuMgRMKvf', 'MIXTA', 'principal'),  -- 12
    (md5('1VzUgWnFFv1VmFk_DVTZ7Z6aOr9qDSwpt'), 'Supervisor Breyman', '1VzUgWnFFv1VmFk_DVTZ7Z6aOr9qDSwpt', 'MIXTA', 'principal'),  -- 11
    (md5('1fZw7kbpMIfWZchsvspEx34i3UgCPCOHw'), 'GPS Javier Gustavo', '1fZw7kbpMIfWZchsvspEx34i3UgCPCOHw', 'MIXTA', 'principal'),  -- 9
    (md5('12QEihV4SQI-aiTeg8Wf1CiWGn2WDNu__'), 'Luis Verdecia', '12QEihV4SQI-aiTeg8Wf1CiWGn2WDNu__', 'MIXTA', 'principal'),  -- 5
    (md5('1B8djtZ3_7L84FzjDElUpR--8YAk058aM'), 'TABLET2', '1B8djtZ3_7L84FzjDElUpR--8YAk058aM', 'MIXTA', 'principal'),  -- 2
    (md5('18bCyG2PFjA6xis-FhQAjLax3hrx-7CdT'), 'TABLET3', '18bCyG2PFjA6xis-FhQAjLax3hrx-7CdT', 'MIXTA', 'principal'),  -- 2
    (md5('1TQjuKJZW1vgmfc8rIBvVwyoJ-5zkR8G5'), 'ERROR', '1TQjuKJZW1vgmfc8rIBvVwyoJ-5zkR8G5', 'MIXTA', 'principal'),  -- 0
    (md5('1xIdvsYVJ4xmS33oI_SGcs6QwRw1DFqCv'), 'GPS Diana Acosta', '1xIdvsYVJ4xmS33oI_SGcs6QwRw1DFqCv', 'MIXTA', 'principal'),  -- 0
    (md5('1qBSnNoDHLrG4LEdyNuGQKe4WUYUtDdLr'), 'GPS supervisor', '1qBSnNoDHLrG4LEdyNuGQKe4WUYUtDdLr', 'MIXTA', 'principal'),  -- 0
    (md5('1RBNJJrtS5eep1G16usOzvSvkTILkTMJK'), 'GPSAdrian', '1RBNJJrtS5eep1G16usOzvSvkTILkTMJK', 'MIXTA', 'principal'),  -- 0
    (md5('1WKwM2OEYTsRmDj0-8vWANtv7cv7TKVKT'), 'GPSHumberto', '1WKwM2OEYTsRmDj0-8vWANtv7cv7TKVKT', 'MIXTA', 'principal'),  -- 0
    (md5('1L4pcOZJlcmccJX080cEyaAj0M13HXTCt'), 'GPSJose Carlos', '1L4pcOZJlcmccJX080cEyaAj0M13HXTCt', 'MIXTA', 'principal'),  -- 0
    (md5('1ocl_R2DhnGJuPYBnYi7YbdX9q5R8GNIS'), 'GPSLisandra', '1ocl_R2DhnGJuPYBnYi7YbdX9q5R8GNIS', 'MIXTA', 'principal'),  -- 0
    (md5('1UZ93y1rxFOeGsTMxqwneRsDPlvP3oZzo'), 'GPSLorenzo', '1UZ93y1rxFOeGsTMxqwneRsDPlvP3oZzo', 'MIXTA', 'principal'),  -- 0
    (md5('1o0JPj4GaJM5qf1f4Eg3QFW5Tgcj5UqXw'), 'GPSMaria', '1o0JPj4GaJM5qf1f4Eg3QFW5Tgcj5UqXw', 'MIXTA', 'principal'),  -- 0
    (md5('1SPMw7LrekDerLyTwU9fo_xkTt3fZJ_gk'), 'GPSNiurka', '1SPMw7LrekDerLyTwU9fo_xkTt3fZJ_gk', 'MIXTA', 'principal'),  -- 0
    (md5('1gLkSoyzWL1_Gri2FyJjpzyhIfE3RjyRT'), 'GPSRafael', '1gLkSoyzWL1_Gri2FyJjpzyhIfE3RjyRT', 'MIXTA', 'principal'),  -- 0
    (md5('1rfmKEXp36-cdOmD5n6UMetFIoDeVKrTA'), 'GPSYanior', '1rfmKEXp36-cdOmD5n6UMetFIoDeVKrTA', 'MIXTA', 'principal'),  -- 0
    (md5('1tflfqfMjBpbx7AJG7tY96jonI-Xkfr7-'), 'GTMDanisley', '1tflfqfMjBpbx7AJG7tY96jonI-Xkfr7-', 'MIXTA', 'principal'),  -- 0
    (md5('18xu5crvQfsk59vx2K7dwoAH3CT1fZKIL'), 'GTMWalter', '18xu5crvQfsk59vx2K7dwoAH3CT1fZKIL', 'MIXTA', 'principal'),  -- 0
    (md5('16pkVIjuy7lLY4Nq7E1v7cKoa8QMz5FDK'), 'Pedidos Rigoberto', '16pkVIjuy7lLY4Nq7E1v7cKoa8QMz5FDK', 'MIXTA', 'principal')  -- 0
ON CONFLICT (folder_id) DO NOTHING;
