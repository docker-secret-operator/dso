// Fixed vectors of BL-02 design revision 6.3 (SHA-256 54ea9717c041750620b8c0fa656ca9bea681ef7025cde88bda3207ad42dbf81a),
// sections 31, 45 and 49, AMENDED FOR SCHEMA VERSION 3 (S2.2, docs/design/m1-ledger-reconciliation.md
// section 1.3). Do not regenerate from the code under test. The amendment was made deliberately:
//   - V1, V2, V4, V5, V6, V7: only schema_version 2 -> 3, the removed "rotation":null and "recoveries":[]
//     fields and the resulting checksums changed (checked mechanically against the revision 6.3 text);
//   - V9: additionally every replica gained rotation_id/delivery/adoption (verified delivery);
//   - V10: the in_progress entry with its intent and prior replicas became a pending entry with two
//     requested-delivery replicas, and the recovery records were removed (no recovery journal);
//   - the section 31 example: same transformation; the rolled-back replica's reason is now the closed
//     vocabulary value "rolled_back";
//   - V8 (recovery-record order) and the V10 planned/done negative controls were removed with the
//     recovery journal.
// Every checksum and the example length were verified by an independent Python canonicalisation
// (sha256 over the document text with body_sha256 emptied), not only by this package.

package ledger

const (
	vecV1Doc           = `{"schema_version":3,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-0123456789abcdef","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"4501b26e824b37f8fdc2c52ab97d6c182ded0c7c812ba73a893a40a4e7b9591d","entries":[]}`
	vecV1SHA           = "4501b26e824b37f8fdc2c52ab97d6c182ded0c7c812ba73a893a40a4e7b9591d"
	vecV2Doc           = `{"schema_version":3,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-0123456789abcdef","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"b50810e347a7bf0d738c9f32072b47c93bd81317a67686b1bdebedc5208d5efc","entries":[{"key":{"project":"","service":"svc","provider":"p","secret":"s"},"desired":null,"desired_seq":0,"applied":null,"state":"idle","replicas":[],"request":null,"last_outcome":null}]}`
	vecV2SHA           = "b50810e347a7bf0d738c9f32072b47c93bd81317a67686b1bdebedc5208d5efc"
	vecV4Doc           = `{"schema_version":3,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-0123456789abcdef","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"9bf22b982656cfcde9df7851daab954eefb0f17d3a70e060c3e22b8e6132dd42","entries":[{"key":{"project":"p<1>","service":"svc&","provider":"p","secret":"dbé\u2028😀"},"desired":null,"desired_seq":0,"applied":null,"state":"idle","replicas":[],"request":null,"last_outcome":null}]}`
	vecV4SHA           = "9bf22b982656cfcde9df7851daab954eefb0f17d3a70e060c3e22b8e6132dd42"
	vecV5Doc           = `{"schema_version":3,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-0123456789abcdef","created_at":"2026-11-12T08:00:00.12Z","updated_at":"2026-11-12T08:00:00.123456789Z","body_sha256":"f8bbbe4bd8459fe12871c8a351bca0587508ad69a4bafeb860c6d82c57e6b85c","entries":[]}`
	vecV5SHA           = "f8bbbe4bd8459fe12871c8a351bca0587508ad69a4bafeb860c6d82c57e6b85c"
	vecV6Doc           = `{"schema_version":3,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-0123456789abcdef","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"f5752617571f67a045334a2f975fa3e3cb560657f517c1b88999f698c3ed7af0","entries":[{"key":{"project":"","service":"a","provider":"p","secret":"s"},"desired":null,"desired_seq":0,"applied":null,"state":"idle","replicas":[],"request":null,"last_outcome":null},{"key":{"project":"","service":"b","provider":"p","secret":"s"},"desired":null,"desired_seq":0,"applied":null,"state":"idle","replicas":[],"request":null,"last_outcome":null}]}`
	vecV6SHA           = "f5752617571f67a045334a2f975fa3e3cb560657f517c1b88999f698c3ed7af0"
	vecV7Doc           = `{"schema_version":3,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-0123456789abcdef","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"00000000824b37f8fdc2c52ab97d6c182ded0c7c812ba73a893a40a4e7b9591d","entries":[]}`
	vecV9Doc           = `{"schema_version":3,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-1b735e3eb74b5d59","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"767d80f15f1f76b680d27af7c94a45f699c7431da7d5b7d534c4a41bb293a8da","entries":[{"key":{"project":"shop","service":"api","provider":"aws","secret":"db"},"desired":{"kind":"hmac","id":"dsdIlrvW_Kod7jRvFZtZoEH02V3YH5Rmt8voWx_c4HI","key_id":"k-1b735e3eb74b5d59","source":""},"desired_seq":1,"applied":{"kind":"hmac","id":"dsdIlrvW_Kod7jRvFZtZoEH02V3YH5Rmt8voWx_c4HI","key_id":"k-1b735e3eb74b5d59","source":""},"state":"idle","replicas":[{"number":"1","container_id":"c1","container_name":"shop-api-1","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"dsdIlrvW_Kod7jRvFZtZoEH02V3YH5Rmt8voWx_c4HI","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z","rotation_id":"rot-0000000000000001","delivery":"verified","adoption":"verified"},{"number":"2","container_id":"c2","container_name":"shop-api-2","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"dsdIlrvW_Kod7jRvFZtZoEH02V3YH5Rmt8voWx_c4HI","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z","rotation_id":"rot-0000000000000001","delivery":"verified","adoption":"verified"}],"request":null,"last_outcome":null}]}`
	vecV9SHA           = "767d80f15f1f76b680d27af7c94a45f699c7431da7d5b7d534c4a41bb293a8da"
	vecExampleIndented = `{
  "schema_version": 3,
  "ledger_id": "5b1f0c9e7a2d4c3b8e6f1a0d9c8b7a65",
  "generation": 42,
  "hmac_key_id": "k-1b735e3eb74b5d59",
  "created_at": "2026-11-12T08:00:00Z",
  "updated_at": "2026-11-12T10:15:31.220118Z",
  "body_sha256": "850da971100dacf53374a51ef994b5da829db96df098f8d86723aaa7ff39a11e",
  "entries": [
    {
      "key": {"project": "", "service": "legacy-batch", "provider": "aws", "secret": "kv/batch"},
      "desired": {"kind": "hmac", "id": "fJnm5qm-pvJ7xfgnFF2082VCI9j5pOJ7NjyTPQjEBBM", "key_id": "k-1b735e3eb74b5d59", "source": ""},
      "desired_seq": 3,
      "applied": {"kind": "hmac", "id": "MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs", "key_id": "k-1b735e3eb74b5d59", "source": ""},
      "state": "pending",
      "replicas": [
        {
          "number": "",
          "container_id": "deadbeef00000000000000000000000000000000000000000000000000000009",
          "container_name": "legacy-batch",
          "created": "2026-11-12T06:00:00Z",
          "applied": {"kind": "hmac", "id": "MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs", "key_id": "k-1b735e3eb74b5d59", "source": ""},
          "outcome": "pending",
          "reason": "delivery_requested",
          "at": "2026-11-12T10:20:00Z",
          "rotation_id": "rot-a1b2c3d4e5f60718",
          "delivery": "requested",
          "adoption": "verified"
        }
      ],
      "request": null,
      "last_outcome": {"kind": "succeeded", "reason": "", "at": "2026-11-12T06:00:05Z", "strategy": "restart"}
    },
    {
      "key": {"project": "shop", "service": "api", "provider": "aws", "secret": "prod/db_password"},
      "desired": {"kind": "hmac", "id": "wDiAJptsGliXVzbtPt8xz6an3gfc6L6oOxlfbFQHlgo", "key_id": "k-1b735e3eb74b5d59", "source": ""},
      "desired_seq": 7,
      "desired_observed_at": "2026-11-12T10:15:30.001Z",
      "applied": {"kind": "hmac", "id": "BxovSblWcAAikYLL8xfyQDDjr-3ydJbVOyHDM0UdV_M", "key_id": "k-1b735e3eb74b5d59", "source": ""},
      "state": "partial",
      "replicas": [
        {
          "number": "1",
          "container_id": "0000000000000000000000000000000000000000000000000000000000000001",
          "container_name": "shop-api-1",
          "created": "2026-11-12T10:15:12.5Z",
          "applied": {"kind": "hmac", "id": "wDiAJptsGliXVzbtPt8xz6an3gfc6L6oOxlfbFQHlgo", "key_id": "k-1b735e3eb74b5d59", "source": ""},
          "outcome": "succeeded",
          "reason": "delivery_verified",
          "at": "2026-11-12T10:15:31Z",
          "rotation_id": "rot-0f1e2d3c4b5a6978",
          "delivery": "verified",
          "adoption": "verified"
        },
        {
          "number": "2",
          "container_id": "0000000000000000000000000000000000000000000000000000000000000002",
          "container_name": "shop-api-2",
          "created": "2026-11-12T10:15:20.7Z",
          "applied": {"kind": "hmac", "id": "wDiAJptsGliXVzbtPt8xz6an3gfc6L6oOxlfbFQHlgo", "key_id": "k-1b735e3eb74b5d59", "source": ""},
          "outcome": "succeeded",
          "reason": "delivery_verified",
          "at": "2026-11-12T10:15:31.2Z",
          "rotation_id": "rot-0f1e2d3c4b5a6978",
          "delivery": "verified",
          "adoption": "verified"
        },
        {
          "number": "3",
          "container_id": "0000000000000000000000000000000000000000000000000000000000000003",
          "container_name": "shop-api-3",
          "created": "2026-11-12T06:00:00Z",
          "applied": {"kind": "hmac", "id": "BxovSblWcAAikYLL8xfyQDDjr-3ydJbVOyHDM0UdV_M", "key_id": "k-1b735e3eb74b5d59", "source": ""},
          "outcome": "rolled_back",
          "reason": "rolled_back",
          "at": "2026-11-12T10:15:45Z",
          "rotation_id": "rot-0f1e2d3c4b5a6978",
          "delivery": "none",
          "adoption": "verified"
        }
      ],
      "request": null,
      "last_outcome": {"kind": "partial", "reason": "1 of 3 replicas rolled back", "at": "2026-11-12T10:15:45Z", "strategy": "rolling"}
    }
  ]
}`
	vecExampleSHA      = "850da971100dacf53374a51ef994b5da829db96df098f8d86723aaa7ff39a11e"
	vecExampleLen      = 3086
	vecV10Doc          = `{"schema_version":3,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-1b735e3eb74b5d59","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"63f0bcc163b5fadcebd2edcec25178d1e1e005435561b005824738b934eeeeb8","entries":[{"key":{"project":"","service":"legacy","provider":"aws","secret":"kv/batch"},"desired":{"kind":"hmac","id":"fJnm5qm-pvJ7xfgnFF2082VCI9j5pOJ7NjyTPQjEBBM","key_id":"k-1b735e3eb74b5d59","source":""},"desired_seq":3,"applied":{"kind":"hmac","id":"MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs","key_id":"k-1b735e3eb74b5d59","source":""},"state":"pending","replicas":[{"number":"","container_id":"c-leg-1","container_name":"legacy-1","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"pending","reason":"delivery_requested","at":"2026-11-12T08:00:00Z","rotation_id":"rot-0000000000000002","delivery":"requested","adoption":"verified"},{"number":"","container_id":"c-leg-2","container_name":"legacy-2","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"pending","reason":"delivery_requested","at":"2026-11-12T08:00:00Z","rotation_id":"rot-0000000000000002","delivery":"requested","adoption":"verified"}],"request":null,"last_outcome":null},{"key":{"project":"shop","service":"api","provider":"aws","secret":"db"},"desired":{"kind":"hmac","id":"T1KFuRy1YenN7U5JdbNDYIJ718ZeMxv_J0yA4h_4H0w","key_id":"k-1b735e3eb74b5d59","source":""},"desired_seq":7,"applied":{"kind":"hmac","id":"dsdIlrvW_Kod7jRvFZtZoEH02V3YH5Rmt8voWx_c4HI","key_id":"k-1b735e3eb74b5d59","source":""},"state":"partial","replicas":[{"number":"","container_id":"c-api-1","container_name":"shop-api-1","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"T1KFuRy1YenN7U5JdbNDYIJ718ZeMxv_J0yA4h_4H0w","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z","rotation_id":"rot-0000000000000001","delivery":"verified","adoption":"verified"},{"number":"","container_id":"c-api-2","container_name":"shop-api-2","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"T1KFuRy1YenN7U5JdbNDYIJ718ZeMxv_J0yA4h_4H0w","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z","rotation_id":"rot-0000000000000001","delivery":"verified","adoption":"verified"},{"number":"","container_id":"c-api-3","container_name":"shop-api-3","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"dsdIlrvW_Kod7jRvFZtZoEH02V3YH5Rmt8voWx_c4HI","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"rolled_back","reason":"","at":"2026-11-12T08:00:00Z","rotation_id":"","delivery":"none","adoption":"unknown"}],"request":null,"last_outcome":{"kind":"partial","reason":"1 of 3 replicas rolled back","at":"2026-11-12T08:00:00Z","strategy":"rolling"}}]}`
	vecV10ReversedFile = `{"schema_version":3,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-1b735e3eb74b5d59","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"63f0bcc163b5fadcebd2edcec25178d1e1e005435561b005824738b934eeeeb8","entries":[{"key":{"project":"shop","service":"api","provider":"aws","secret":"db"},"desired":{"kind":"hmac","id":"T1KFuRy1YenN7U5JdbNDYIJ718ZeMxv_J0yA4h_4H0w","key_id":"k-1b735e3eb74b5d59","source":""},"desired_seq":7,"applied":{"kind":"hmac","id":"dsdIlrvW_Kod7jRvFZtZoEH02V3YH5Rmt8voWx_c4HI","key_id":"k-1b735e3eb74b5d59","source":""},"state":"partial","replicas":[{"number":"","container_id":"c-api-3","container_name":"shop-api-3","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"dsdIlrvW_Kod7jRvFZtZoEH02V3YH5Rmt8voWx_c4HI","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"rolled_back","reason":"","at":"2026-11-12T08:00:00Z","rotation_id":"","delivery":"none","adoption":"unknown"},{"number":"","container_id":"c-api-2","container_name":"shop-api-2","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"T1KFuRy1YenN7U5JdbNDYIJ718ZeMxv_J0yA4h_4H0w","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z","rotation_id":"rot-0000000000000001","delivery":"verified","adoption":"verified"},{"number":"","container_id":"c-api-1","container_name":"shop-api-1","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"T1KFuRy1YenN7U5JdbNDYIJ718ZeMxv_J0yA4h_4H0w","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z","rotation_id":"rot-0000000000000001","delivery":"verified","adoption":"verified"}],"request":null,"last_outcome":{"kind":"partial","reason":"1 of 3 replicas rolled back","at":"2026-11-12T08:00:00Z","strategy":"rolling"}},{"key":{"project":"","service":"legacy","provider":"aws","secret":"kv/batch"},"desired":{"kind":"hmac","id":"fJnm5qm-pvJ7xfgnFF2082VCI9j5pOJ7NjyTPQjEBBM","key_id":"k-1b735e3eb74b5d59","source":""},"desired_seq":3,"applied":{"kind":"hmac","id":"MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs","key_id":"k-1b735e3eb74b5d59","source":""},"state":"pending","replicas":[{"number":"","container_id":"c-leg-2","container_name":"legacy-2","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"pending","reason":"delivery_requested","at":"2026-11-12T08:00:00Z","rotation_id":"rot-0000000000000002","delivery":"requested","adoption":"verified"},{"number":"","container_id":"c-leg-1","container_name":"legacy-1","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"pending","reason":"delivery_requested","at":"2026-11-12T08:00:00Z","rotation_id":"rot-0000000000000002","delivery":"requested","adoption":"verified"}],"request":null,"last_outcome":null}]}`
	vecV10SHA          = "63f0bcc163b5fadcebd2edcec25178d1e1e005435561b005824738b934eeeeb8"
	vecFixtureKeyID    = "k-1b735e3eb74b5d59"
)
