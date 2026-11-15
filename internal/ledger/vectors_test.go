// Code generated from BL-02 design revision 6.3 (SHA-256 54ea9717c041750620b8c0fa656ca9bea681ef7025cde88bda3207ad42dbf81a); DO NOT EDIT.
// Fixed vectors: every value below is copied byte for byte from sections 31, 45 and 49 of that
// document. Tests assert against these constants; they are never regenerated from the code under test.

package ledger

const (
	vecV1Doc           = `{"schema_version":2,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-0123456789abcdef","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"79cb73fc5bf1a564f31b8b4ff6375cd71b4fad71dfa447c6b1697a1a157efdba","entries":[],"recoveries":[]}`
	vecV1SHA           = "79cb73fc5bf1a564f31b8b4ff6375cd71b4fad71dfa447c6b1697a1a157efdba"
	vecV2Doc           = `{"schema_version":2,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-0123456789abcdef","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"fe4fe2c0b9a87f9eac7045224f747ab32dce787a1bc841bfb6ba780f0b4f975d","entries":[{"key":{"project":"","service":"svc","provider":"p","secret":"s"},"desired":null,"desired_seq":0,"applied":null,"state":"idle","replicas":[],"rotation":null,"request":null,"last_outcome":null}],"recoveries":[]}`
	vecV2SHA           = "fe4fe2c0b9a87f9eac7045224f747ab32dce787a1bc841bfb6ba780f0b4f975d"
	vecV4Doc           = `{"schema_version":2,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-0123456789abcdef","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"4bf644e341d3aebd1e1f3d3233f87be5fbe314eeef31ffe021b501acbb6319b0","entries":[{"key":{"project":"p<1>","service":"svc&","provider":"p","secret":"dbé\u2028😀"},"desired":null,"desired_seq":0,"applied":null,"state":"idle","replicas":[],"rotation":null,"request":null,"last_outcome":null}],"recoveries":[]}`
	vecV4SHA           = "4bf644e341d3aebd1e1f3d3233f87be5fbe314eeef31ffe021b501acbb6319b0"
	vecV5Doc           = `{"schema_version":2,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-0123456789abcdef","created_at":"2026-11-12T08:00:00.12Z","updated_at":"2026-11-12T08:00:00.123456789Z","body_sha256":"a6b1fbeca87bfdb366ee356f6c4eb29fd18edf4aef2e3b77a327e6c51cef6a1c","entries":[],"recoveries":[]}`
	vecV5SHA           = "a6b1fbeca87bfdb366ee356f6c4eb29fd18edf4aef2e3b77a327e6c51cef6a1c"
	vecV6Doc           = `{"schema_version":2,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-0123456789abcdef","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"26bcf93a865ba6853e0fc1d629cfed49120d9cd8ede4e72795443689d1c73177","entries":[{"key":{"project":"","service":"a","provider":"p","secret":"s"},"desired":null,"desired_seq":0,"applied":null,"state":"idle","replicas":[],"rotation":null,"request":null,"last_outcome":null},{"key":{"project":"","service":"b","provider":"p","secret":"s"},"desired":null,"desired_seq":0,"applied":null,"state":"idle","replicas":[],"rotation":null,"request":null,"last_outcome":null}],"recoveries":[]}`
	vecV6SHA           = "26bcf93a865ba6853e0fc1d629cfed49120d9cd8ede4e72795443689d1c73177"
	vecV7Doc           = `{"schema_version":2,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-0123456789abcdef","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"000000005bf1a564f31b8b4ff6375cd71b4fad71dfa447c6b1697a1a157efdba","entries":[],"recoveries":[]}`
	vecV8Doc           = `{"schema_version":2,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-0123456789abcdef","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"b81b76f0b4be4bb8311f894d8990b5491d008d8870d7f4288f455f4f3f2ba1af","entries":[],"recoveries":[{"recovery_id":"11111111111111111111111111111111","scope":{"project":"p","service":"s1","container_name":""},"trigger":"orphan_scan","planned":[],"done":[],"state":"planned","started_at":"2026-11-12T08:00:01Z"},{"recovery_id":"22222222222222222222222222222222","scope":{"project":"p","service":"s2","container_name":""},"trigger":"orphan_scan","planned":[],"done":[],"state":"planned","started_at":"2026-11-12T08:00:02Z"}]}`
	vecV8SHA           = "b81b76f0b4be4bb8311f894d8990b5491d008d8870d7f4288f455f4f3f2ba1af"
	vecV9Doc           = `{"schema_version":2,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-1b735e3eb74b5d59","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"503b3c6bb49968dd0afa2e0aed64e6dab7110dd34497b17cb148be784d561ed6","entries":[{"key":{"project":"shop","service":"api","provider":"aws","secret":"db"},"desired":{"kind":"hmac","id":"dsdIlrvW_Kod7jRvFZtZoEH02V3YH5Rmt8voWx_c4HI","key_id":"k-1b735e3eb74b5d59","source":""},"desired_seq":1,"applied":{"kind":"hmac","id":"dsdIlrvW_Kod7jRvFZtZoEH02V3YH5Rmt8voWx_c4HI","key_id":"k-1b735e3eb74b5d59","source":""},"state":"idle","replicas":[{"number":"1","container_id":"c1","container_name":"shop-api-1","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"dsdIlrvW_Kod7jRvFZtZoEH02V3YH5Rmt8voWx_c4HI","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z"},{"number":"2","container_id":"c2","container_name":"shop-api-2","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"dsdIlrvW_Kod7jRvFZtZoEH02V3YH5Rmt8voWx_c4HI","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z"}],"rotation":null,"request":null,"last_outcome":null}],"recoveries":[]}`
	vecV9SHA           = "503b3c6bb49968dd0afa2e0aed64e6dab7110dd34497b17cb148be784d561ed6"
	vecExampleIndented = `{
  "schema_version": 2,
  "ledger_id": "5b1f0c9e7a2d4c3b8e6f1a0d9c8b7a65",
  "generation": 42,
  "hmac_key_id": "k-1b735e3eb74b5d59",
  "created_at": "2026-11-12T08:00:00Z",
  "updated_at": "2026-11-12T10:15:31.220118Z",
  "body_sha256": "7c5a8c5cb21d5bae5f780783e78a389fe95e62c0b049572e18db147263f5f26f",
  "entries": [
    {
      "key": {"project": "", "service": "legacy-batch", "provider": "aws", "secret": "kv/batch"},
      "desired": {"kind": "hmac", "id": "fJnm5qm-pvJ7xfgnFF2082VCI9j5pOJ7NjyTPQjEBBM", "key_id": "k-1b735e3eb74b5d59", "source": ""},
      "desired_seq": 3,
      "applied": {"kind": "hmac", "id": "MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs", "key_id": "k-1b735e3eb74b5d59", "source": ""},
      "state": "in_progress",
      "replicas": [
        {
          "number": "",
          "container_id": "deadbeef00000000000000000000000000000000000000000000000000000009",
          "container_name": "legacy-batch",
          "created": "2026-11-12T06:00:00Z",
          "applied": {"kind": "hmac", "id": "MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs", "key_id": "k-1b735e3eb74b5d59", "source": ""},
          "outcome": "succeeded",
          "reason": "",
          "at": "2026-11-12T06:00:05Z"
        }
      ],
      "rotation": {
        "rotation_id": "a1b2c3d4e5f60718293a4b5c6d7e8f90",
        "started_at": "2026-11-12T10:20:00Z",
        "desired": {"kind": "hmac", "id": "fJnm5qm-pvJ7xfgnFF2082VCI9j5pOJ7NjyTPQjEBBM", "key_id": "k-1b735e3eb74b5d59", "source": ""},
        "strategy": "restart",
        "prior_replicas": [
          {
            "number": "",
            "container_id": "deadbeef00000000000000000000000000000000000000000000000000000009",
            "container_name": "legacy-batch",
            "created": "2026-11-12T06:00:00Z",
            "applied": {"kind": "hmac", "id": "MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs", "key_id": "k-1b735e3eb74b5d59", "source": ""},
            "outcome": "succeeded",
            "reason": "",
            "at": "2026-11-12T06:00:05Z"
          }
        ],
        "supersedes": ""
      },
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
          "reason": "",
          "at": "2026-11-12T10:15:31Z"
        },
        {
          "number": "2",
          "container_id": "0000000000000000000000000000000000000000000000000000000000000002",
          "container_name": "shop-api-2",
          "created": "2026-11-12T10:15:20.7Z",
          "applied": {"kind": "hmac", "id": "wDiAJptsGliXVzbtPt8xz6an3gfc6L6oOxlfbFQHlgo", "key_id": "k-1b735e3eb74b5d59", "source": ""},
          "outcome": "succeeded",
          "reason": "",
          "at": "2026-11-12T10:15:31.2Z"
        },
        {
          "number": "3",
          "container_id": "0000000000000000000000000000000000000000000000000000000000000003",
          "container_name": "shop-api-3",
          "created": "2026-11-12T06:00:00Z",
          "applied": {"kind": "hmac", "id": "BxovSblWcAAikYLL8xfyQDDjr-3ydJbVOyHDM0UdV_M", "key_id": "k-1b735e3eb74b5d59", "source": ""},
          "outcome": "rolled_back",
          "reason": "health verification failed",
          "at": "2026-11-12T10:15:45Z"
        }
      ],
      "rotation": null,
      "request": null,
      "last_outcome": {"kind": "partial", "reason": "1 of 3 replicas rolled back", "at": "2026-11-12T10:15:45Z", "strategy": "rolling"}
    }
  ],
  "recoveries": []
}`
	vecExampleSHA       = "7c5a8c5cb21d5bae5f780783e78a389fe95e62c0b049572e18db147263f5f26f"
	vecExampleLen       = 3381
	vecV10Doc           = `{"schema_version":2,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-1b735e3eb74b5d59","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"2265dba9fe045f097c4734bf8942a92baa570348b51f3cc1f1612f1de12bfa18","entries":[{"key":{"project":"","service":"legacy","provider":"aws","secret":"kv/batch"},"desired":{"kind":"hmac","id":"fJnm5qm-pvJ7xfgnFF2082VCI9j5pOJ7NjyTPQjEBBM","key_id":"k-1b735e3eb74b5d59","source":""},"desired_seq":3,"applied":{"kind":"hmac","id":"MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs","key_id":"k-1b735e3eb74b5d59","source":""},"state":"in_progress","replicas":[{"number":"","container_id":"c-leg-1","container_name":"legacy-1","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z"},{"number":"","container_id":"c-leg-2","container_name":"legacy-2","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z"}],"rotation":{"rotation_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","started_at":"2026-11-12T08:00:00Z","desired":{"kind":"hmac","id":"fJnm5qm-pvJ7xfgnFF2082VCI9j5pOJ7NjyTPQjEBBM","key_id":"k-1b735e3eb74b5d59","source":""},"strategy":"restart","prior_replicas":[{"number":"","container_id":"c-leg-1","container_name":"legacy-1","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z"},{"number":"","container_id":"c-leg-2","container_name":"legacy-2","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z"}],"supersedes":""},"request":null,"last_outcome":null},{"key":{"project":"shop","service":"api","provider":"aws","secret":"db"},"desired":{"kind":"hmac","id":"T1KFuRy1YenN7U5JdbNDYIJ718ZeMxv_J0yA4h_4H0w","key_id":"k-1b735e3eb74b5d59","source":""},"desired_seq":7,"applied":{"kind":"hmac","id":"dsdIlrvW_Kod7jRvFZtZoEH02V3YH5Rmt8voWx_c4HI","key_id":"k-1b735e3eb74b5d59","source":""},"state":"partial","replicas":[{"number":"","container_id":"c-api-1","container_name":"shop-api-1","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"T1KFuRy1YenN7U5JdbNDYIJ718ZeMxv_J0yA4h_4H0w","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z"},{"number":"","container_id":"c-api-2","container_name":"shop-api-2","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"T1KFuRy1YenN7U5JdbNDYIJ718ZeMxv_J0yA4h_4H0w","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z"},{"number":"","container_id":"c-api-3","container_name":"shop-api-3","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"dsdIlrvW_Kod7jRvFZtZoEH02V3YH5Rmt8voWx_c4HI","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"rolled_back","reason":"","at":"2026-11-12T08:00:00Z"}],"rotation":null,"request":null,"last_outcome":{"kind":"partial","reason":"1 of 3 replicas rolled back","at":"2026-11-12T08:00:00Z","strategy":"rolling"}}],"recoveries":[{"recovery_id":"11111111111111111111111111111111","scope":{"project":"p","service":"s1","container_name":""},"trigger":"orphan_scan","planned":[{"op":"stop","container_id":"c-x1","container_name":"svc_dso_new_","created":"2026-11-12T06:00:00Z"},{"op":"remove","container_id":"c-x1","container_name":"svc_dso_new_","created":"2026-11-12T06:00:00Z"},{"op":"start","container_id":"c-x2","container_name":"svc","created":"2026-11-12T06:00:00Z"}],"done":[{"outcome":"ok","reason":""},{"outcome":"already_done","reason":""}],"state":"in_progress","started_at":"2026-11-12T08:00:01Z"},{"recovery_id":"22222222222222222222222222222222","scope":{"project":"p","service":"s2","container_name":""},"trigger":"ledger_intent","planned":[{"op":"remove","container_id":"c-y1","container_name":"svc2_dso_backup_","created":"2026-11-12T06:00:00Z"}],"done":[],"state":"planned","started_at":"2026-11-12T08:00:02Z"}]}`
	vecV10ReversedFile  = `{"schema_version":2,"ledger_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":1,"hmac_key_id":"k-1b735e3eb74b5d59","created_at":"2026-11-12T08:00:00Z","updated_at":"2026-11-12T08:00:00Z","body_sha256":"2265dba9fe045f097c4734bf8942a92baa570348b51f3cc1f1612f1de12bfa18","entries":[{"key":{"project":"shop","service":"api","provider":"aws","secret":"db"},"desired":{"kind":"hmac","id":"T1KFuRy1YenN7U5JdbNDYIJ718ZeMxv_J0yA4h_4H0w","key_id":"k-1b735e3eb74b5d59","source":""},"desired_seq":7,"applied":{"kind":"hmac","id":"dsdIlrvW_Kod7jRvFZtZoEH02V3YH5Rmt8voWx_c4HI","key_id":"k-1b735e3eb74b5d59","source":""},"state":"partial","replicas":[{"number":"","container_id":"c-api-3","container_name":"shop-api-3","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"dsdIlrvW_Kod7jRvFZtZoEH02V3YH5Rmt8voWx_c4HI","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"rolled_back","reason":"","at":"2026-11-12T08:00:00Z"},{"number":"","container_id":"c-api-2","container_name":"shop-api-2","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"T1KFuRy1YenN7U5JdbNDYIJ718ZeMxv_J0yA4h_4H0w","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z"},{"number":"","container_id":"c-api-1","container_name":"shop-api-1","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"T1KFuRy1YenN7U5JdbNDYIJ718ZeMxv_J0yA4h_4H0w","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z"}],"rotation":null,"request":null,"last_outcome":{"kind":"partial","reason":"1 of 3 replicas rolled back","at":"2026-11-12T08:00:00Z","strategy":"rolling"}},{"key":{"project":"","service":"legacy","provider":"aws","secret":"kv/batch"},"desired":{"kind":"hmac","id":"fJnm5qm-pvJ7xfgnFF2082VCI9j5pOJ7NjyTPQjEBBM","key_id":"k-1b735e3eb74b5d59","source":""},"desired_seq":3,"applied":{"kind":"hmac","id":"MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs","key_id":"k-1b735e3eb74b5d59","source":""},"state":"in_progress","replicas":[{"number":"","container_id":"c-leg-2","container_name":"legacy-2","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z"},{"number":"","container_id":"c-leg-1","container_name":"legacy-1","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z"}],"rotation":{"rotation_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","started_at":"2026-11-12T08:00:00Z","desired":{"kind":"hmac","id":"fJnm5qm-pvJ7xfgnFF2082VCI9j5pOJ7NjyTPQjEBBM","key_id":"k-1b735e3eb74b5d59","source":""},"strategy":"restart","prior_replicas":[{"number":"","container_id":"c-leg-2","container_name":"legacy-2","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z"},{"number":"","container_id":"c-leg-1","container_name":"legacy-1","created":"2026-11-12T06:00:00Z","applied":{"kind":"hmac","id":"MCndO_BNut27sFUqVqdLV1CLtnkQWQdMDuan1UJfLOs","key_id":"k-1b735e3eb74b5d59","source":""},"outcome":"succeeded","reason":"","at":"2026-11-12T08:00:00Z"}],"supersedes":""},"request":null,"last_outcome":null}],"recoveries":[{"recovery_id":"22222222222222222222222222222222","scope":{"project":"p","service":"s2","container_name":""},"trigger":"ledger_intent","planned":[{"op":"remove","container_id":"c-y1","container_name":"svc2_dso_backup_","created":"2026-11-12T06:00:00Z"}],"done":[],"state":"planned","started_at":"2026-11-12T08:00:02Z"},{"recovery_id":"11111111111111111111111111111111","scope":{"project":"p","service":"s1","container_name":""},"trigger":"orphan_scan","planned":[{"op":"stop","container_id":"c-x1","container_name":"svc_dso_new_","created":"2026-11-12T06:00:00Z"},{"op":"remove","container_id":"c-x1","container_name":"svc_dso_new_","created":"2026-11-12T06:00:00Z"},{"op":"start","container_id":"c-x2","container_name":"svc","created":"2026-11-12T06:00:00Z"}],"done":[{"outcome":"ok","reason":""},{"outcome":"already_done","reason":""}],"state":"in_progress","started_at":"2026-11-12T08:00:01Z"}]}`
	vecV10SHA           = "2265dba9fe045f097c4734bf8942a92baa570348b51f3cc1f1612f1de12bfa18"
	vecV10NegPlannedSHA = "536d9409279951554c3e28218e969e99a42eede75b0f1ca29d9440c9abad0ba1"
	vecV10NegDoneSHA    = "dfb4cce2692162582aa23b93b54ad3a5d8c27a8926708d09c8c72a0f20d6bfc1"
	vecFixtureKeyID     = "k-1b735e3eb74b5d59"
)
