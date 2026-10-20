package db

// legacyToolTextHashes identifies untouched Chinese built-in tool defaults from
// earlier releases. The hashes let startup migrate stock text without retaining
// the old localized prose or overwriting user-edited descriptions.
type legacyToolTextHash struct {
	description           string
	alternateDescriptions []string
	fields                map[string]string
}

var legacyToolTextHashes = map[string]legacyToolTextHash{
	"add_company_scope": {
		description: "c460db4b5834fb3c22d27937a2b87902cd9eaf4246e53e7e44a4081c5d6243f0",
		fields: map[string]string{
			"/properties/company/description": "28a2ace2629a4d22eee7d380ed81c58b09a9663bc5f07f92bdd3940899ddb079",
			"/properties/logo/description":    "3599e6095ba67d5dffed173f633712edbdbdf65c7ae3e8e5d45e3f71e015a7d9",
			"/properties/reason/description":  "4ce91a174d2048e1d4df3c83d4896c4a775c6a9f13d66d1c19f848cf7e164aec",
			"/properties/scope/description":   "bc954950cc134b201a421f6ee42539a75e4229027ae36d6ced1d6b88b4241ab5",
		},
	},
	"add_hint": {
		description: "629c6494669d5d5b53b951c70c949833dc1a885e91c85469caa6a9527b9ca61e",
		fields: map[string]string{
			"/properties/asset_ids/description":                                                       "ad956e02e3accdf3ee5eb28392d1b8374464666c199aa07b300aed9556d01c5a",
			"/properties/hints/description":                                                           "127405b96f12f3ac99c105b1cf63eae7d733e344003e431bc87f65f7e2b745a7",
			"/properties/hints/items/properties/text/description":                                     "5d134b96d369b1a66fd6366629e78c5feed71760cf48b6156dcffe423c03c988",
			"/properties/hints/items/properties/traffic_refs/description":                             "adbe4c230b444dca6465305406ad65741e915e53830ddc7f134850d9830f5843",
			"/properties/hints/items/properties/traffic_refs/items/properties/note/description":       "d45da01df7568286565c3b9abfbd88f6927e72100bd0d6c8b59a1c3f342d36f0",
			"/properties/hints/items/properties/traffic_refs/items/properties/traffic_id/description": "8b0a5da66d7b8a5f781f189c7f49878759bc045c4f179014a3d30c06dc1fb425",
			"/properties/text/description":                                                            "bd67416b24a4906f8e8ab2c0ecb156643300c492ca3ef9126da8d76bf773bbe1",
			"/properties/traffic_refs/description":                                                    "adbe4c230b444dca6465305406ad65741e915e53830ddc7f134850d9830f5843",
			"/properties/traffic_refs/items/properties/note/description":                              "d45da01df7568286565c3b9abfbd88f6927e72100bd0d6c8b59a1c3f342d36f0",
			"/properties/traffic_refs/items/properties/traffic_id/description":                        "8b0a5da66d7b8a5f781f189c7f49878759bc045c4f179014a3d30c06dc1fb425",
		},
	},
	"add_intent": {
		description: "6754abb7254a6a7ec1cf980f00a768d384d473f84d4c90b1e0446963c0a92767",
		fields: map[string]string{
			"/properties/asset_ids/description":  "bac1288e2a51fe17be88938fd58ae6b7d34c03eba3b69ac0887742b78598a4dd",
			"/properties/intents/description":    "ad103eeedb2328ccd7acbc1432535869429ff4bc990d58f30c4f44ead955b929",
			"/properties/parent_ids/description": "575d2ecb1e67c24060c9a69b403801a687c41948a3789bcd8249937f3b144d6e",
			"/properties/priority/description":   "a3530cd0b80d9d9996ca20a808734842d6dc8ce00d0950096a935e0baadfeab3",
			"/properties/summary/description":    "06b9ecb9214a59b3239e8baea1218f6c1ef8d3172db5998e1a3e16e7c520b4cd",
		},
	},
	"add_task_scope": {
		description: "23caa9d2e944fac4bd7f329bd5de45344a0633ed9f554759c4be753e5c9cc9a3",
		fields: map[string]string{
			"/properties/entries/description": "bdb04362e1481f31276c624b1bdd47652779c72050ec9d932aa6a78948ed6d19",
			"/properties/kind/description":    "51e3c6a8ecf3b27aa1edc59e7f94c482dd37e37ffee830d8c8dce1407cb082ba",
			"/properties/reason/description":  "e2f6965155bc9f0ecbaf1ec20ff398d17bff9442d12e6a9b6fb7b27945778089",
			"/properties/value/description":   "4d4e6d36aa0f4799cb14653a5451e0e352a911f50e7489e46dbde1b8bb6df663",
		},
	},
	"expand_digest": {
		description: "e0807f8e20e25f3f682a51c688160942e568ac80ca0d16819e7361a9abe68fb1",
		fields: map[string]string{
			"/properties/id/description": "d0ad77e288c99fc8bfd2a8eee74bea3f5b391b2f7c832609a44ed04822483a04",
		},
	},
	"get_worker_output": {
		description: "f25ffdc9580a39cb8f6aca6c3bde7860934b73b579157533e97ee85ee271f21e",
		fields: map[string]string{
			"/properties/intent_id/description": "30981e888f345551bf326cb34f1d2e301aa0ec8fb1d92e20dc3a4b595eb3406a",
		},
	},
	"get_worker_trace": {
		description: "5931721f9a063a608b1ad5d72c2ca48b0bd763d607769c4d2161f074c23835ec",
		fields: map[string]string{
			"/properties/intent_id/description": "30981e888f345551bf326cb34f1d2e301aa0ec8fb1d92e20dc3a4b595eb3406a",
			"/properties/limit/description":     "3fb02cffd9ff34228b680dc5107b87507104d08bbac43fc6884c1ea49d044613",
			"/properties/q/description":         "42915fae1c36f145fc0e013f49299ac6760b41cd7960203f63b542f2ff80c4ff",
			"/properties/step_ids/description":  "344c3433af314fed3a6bf12d489ec8f9e51f488a9e7e52b6af186a8111e6237d",
		},
	},
	"goal_met": {
		description: "cba19394662c07ae525bcf3e21ead34b671a3e391c4e2a020518d4b096a0ac00",
		fields: map[string]string{
			"/properties/reason/description": "29cc4411fef8fd3dd31c8b23e94c05f0418146ccc4939b641ea5ddf492517979",
		},
	},
	"graph_overview": {
		description: "541280dbe0192fe121e79d956d9bc41553fa2b317a344cf635ee602aa196ada3",
	},
	"insert_assets": {
		description: "c377ce43215f3b6b573b21e301b27180e62a0cc2a480f7b41d54f61692167dbd",
		fields: map[string]string{
			"/properties/assets/description":                                                      "a10071af3125c8dc457c20eb6e26abe0b5237069a579103c0e852412d031eb22",
			"/properties/assets/items/properties/app_icp/description":                             "428a0a071dd7515ae784656c83a16b066ea4fa4c4b44d068f5657ffa26e47065",
			"/properties/assets/items/properties/app_name/description":                            "f5c1456acfe0e4037363275af7a885b08f5cfdc90acff122eed93bfe6aa56b4c",
			"/properties/assets/items/properties/auth/description":                                "97155dd80400d61fb73f2a4b1e0e791ed7246577da1d248ad458a79fb9dd8cbc",
			"/properties/assets/items/properties/bound_domains/description":                       "9882415862c9e88f30f3f1a42aec45bcb656314caf9c044506f6b5461153b248",
			"/properties/assets/items/properties/bundle_id/description":                           "e547ea531c81f3d56eee53643088a3ee076381d1535ac7b9a3e004810ec875d5",
			"/properties/assets/items/properties/category/description":                            "6bc09048a00e6a734e0b61509a5b17ec87ee16a65b785ace5b22b17752f81051",
			"/properties/assets/items/properties/company_id/description":                          "e6aa177ff647cd3a8ddb3764d23afa87cbfc6f4324698dbdc07745d082394ac3",
			"/properties/assets/items/properties/content_length/description":                      "24b8c94e695cc72f81dc23a25e0f045713a4cde81e91ce41fe035c9d0d5bc1c1",
			"/properties/assets/items/properties/description/description":                         "69b8bd375f41c32ce138e93015a598c969dd8a1909bf1165d45b42c56d194001",
			"/properties/assets/items/properties/domain/description":                              "5dbad3d2b5e06cb278f8d5799a2d5dd0c2549e0e2a138ca8b0ec929b7bc5b115",
			"/properties/assets/items/properties/favicon_mmh3/description":                        "2f86d16a9bb1ba2c9732b39a798034bff3679dc68493c3910156faaca04416f4",
			"/properties/assets/items/properties/icp/description":                                 "4cb35afb6ac73f929f9c4709f0969a343bdca09d453e018543d691d63650079d",
			"/properties/assets/items/properties/ip/description":                                  "3e2419fea0f7ac644895fe98e913f6aad83812ef3aff41d01a57d6b0ba29fef6",
			"/properties/assets/items/properties/method/description":                              "46b3a25f1498ed4775f7cff7403b36b8cffcb506aa83042abfde8e7532f7917b",
			"/properties/assets/items/properties/open_ports/description":                          "8ab4b0df9a889784e6f24a2bb5242d28ec2ef8debfe2b2ad2f3383065617148c",
			"/properties/assets/items/properties/open_ports/items/properties/port/description":    "7830f101b9acadf657fdaaca4a9a59ca71d0cf29775eb9b94776e44fc74bf50d",
			"/properties/assets/items/properties/open_ports/items/properties/service/description": "804f88a069e21c85ce11a207c6483d948ec2c131b349af63c40b1b483aba83bd",
			"/properties/assets/items/properties/page_title/description":                          "043fced467d8e2981623fd4f41ef268526b23bb5320d76917a6f06e915c3d070",
			"/properties/assets/items/properties/params/description":                              "a9e009b7855e463f5f1b82505f2e8a8beee905e74999e1eef7690830126f931b",
			"/properties/assets/items/properties/port/description":                                "78fa9b246956f3bc18e0e28eb8fb00f5a4639a76b4b0bdfdf595df2f15668ad7",
			"/properties/assets/items/properties/record_type/description":                         "c49c560257128b1a704b2a23d84692b8f9f1e7cade0c552ad72f34715b0ad499",
			"/properties/assets/items/properties/record_value/description":                        "7a4ca3635b2cb4030f0b4a57dc4f77ea16d1ca6debab4dee6358af3147b79529",
			"/properties/assets/items/properties/service_name/description":                        "ef278c2f8c26277decac763ee93d8ecc966480aed931f772dc096b4fbbe1c20c",
			"/properties/assets/items/properties/status_code/description":                         "3274badff5400b09e32f70206b4cf87ff182ce6956e09b5c5255a09cc8a8afdf",
			"/properties/assets/items/properties/technologies/description":                        "76d6b2011fc754849f75944ce813cf701d6473d34089477b019b40876fc034c1",
			"/properties/assets/items/properties/type/description":                                "0cb05c0d0234ff99e67e434d5c60aa753cf2b4c607d8cabb917dd6d48a75c20b",
			"/properties/assets/items/properties/url/description":                                 "eb410ffb9a36b041f44e2f38b4abac36d04bc9f51446eb10543dfe43bbe04bff",
		},
	},
	"kill_work": {
		description: "34036c67e6fa85c1abe66fb5983e390c97acc525b499d6cad20e907877002db9",
		fields: map[string]string{
			"/properties/intent_id/description": "881f8d12ac893e515aff85150ad78eb92b0f41bff8401f41a3f019568d7a4db2",
		},
	},
	"list_assets": {
		description: "cf302317a50c3f326e7eaefcd0d7f038bac04424d84cf3deeeb602198aa11aff",
		fields: map[string]string{
			"/properties/dsl/description":    "b901cbb7ebad81936be02f728cb026d0b5d063572b6aed84c63ca5a25e8039e4",
			"/properties/id/description":     "046ecfa841df09e3a00f005ea49f9b6d69c946c3b11f615622e50d08177b6a0b",
			"/properties/ids/description":    "4b8ad36c8ee53d7c545ecc90313e4b0b3dabe591bbaf3fce2c7853152aecaaf1",
			"/properties/limit/description":  "bd0da927658effd2aa04faf3076301bc0077e918625086a2d2984c97320e9f82",
			"/properties/offset/description": "2a26d91e662eed02399b83ee9b657066fb97b2ab315821cea59c76f3bc4bd6b3",
			"/properties/type/description":   "c0dea6735ea8a9ecc6b2b272f5b8c518e8820cd3ebcaaa1b19efedfd58f68268",
		},
	},
	"list_companies": {
		description: "100d9924f89e3c50313af8296dafbc16d802733995f32f09cb24709276d2d9a3",
		fields: map[string]string{
			"/properties/search/description": "25486b4fc665f32641c5a8a000a2aab7c1d1cd7fe6652b7c3782766a3b760398",
		},
	},
	"list_facts": {
		description: "367be32e153ea77e11ab5d4fdc25b8e2a1a7b733cc8e4bbe20df592c322283e0",
		fields: map[string]string{
			"/properties/before/description": "4b3084a6c155088df237db8a1cedc3086937438daddb70436d7138d066a4b2e2",
			"/properties/limit/description":  "5e952ac0df5516c5635856eca6784c71ab1ece8f9d131f8082020ccfff1768bb",
			"/properties/q/description":      "bf11cfc7a1a0142fc3927aef1ef29350b6140be248a2129a9ced2951b622ed7b",
		},
	},
	"list_findings": {
		description: "1e25104858ba614987dee500ff60d67e1fe837910f38ea750d9e2d2e8312bc15",
	},
	"list_goals": {
		description: "8badb45c95ec41fd8208f127cf91db19072b586ce25f82893abe5f320aedd433",
	},
	"list_untested_assets": {
		description: "751b0466c5ae59415d9c4a375ea4fc271f7b558f08342c8616d2579b3ee8e0d5",
		fields: map[string]string{
			"/properties/page/description":      "b889eeac448059e3624a3565930f4fad18dd5a26516b144bf77a7e59cf0fdea2",
			"/properties/page_size/description": "416462fbc65570fc8df0e0bce93b5f8e366b28eaabd868ca2aefeebf959142ba",
			"/properties/type/description":      "2ad17bfe8fb74a7f9896c9759756d6be6c837f350a97e18137b9b6efdfbcfc19",
		},
	},
	"node_detail": {
		description: "15b983a6465d518528df5f0042ca1fbfde7d6f6a7e9a8072656c34b982426417",
		fields: map[string]string{
			"/properties/id/description": "ca4e3a00d530cd6600b268af400680188f267b774b1bcf815e090d3d343ae161",
		},
	},
	"prove_goal": {
		description: "8a40e6de058ad1d43cb9a666b045beeb868f8ad0a04320e76edba6d80fbfad66",
		fields: map[string]string{
			"/properties/evidence_id/description": "71392c5bf6447f0abbe2d8686c6a0b6ce17bc2b3ae5bc67550f7c22c793be904",
			"/properties/goal_id/description":     "18a7d66812890471ddd9de688bf3785a06711f0748be4688276d805e901b8661",
			"/properties/reason/description":      "b222b686425741b81fdc60582983a59cd1a925116a77a85004a4a763d3a634ae",
		},
	},
	"record_fact": {
		description: "8b9446c8c11a2cd45f6f5c45bb541a732c399415a718b44b69479e4619078593",
		fields: map[string]string{
			"/properties/asset_ids/description":  "11b5db568402c4f02b795d5437e062bcffea807ca428c0a5a8b78dbe1378e598",
			"/properties/confidence/description": "d6e311e6e2e54e517443092131448f148da65bcf3f80e6715f729eb31e240062",
			"/properties/detail/description":     "578adbc82bb57e5ceed9960d37d4f00ba85bf2fa1a580a790261c4f92abca7bf",
			"/properties/evidence/description":   "fc7eb58e150fd1edad037111ea5a3dde292098c54d583519d359dec929642710",
			"/properties/facts/description":      "5bb8f58ddd62c6af502167b538a5b29728bdcc1ebc1cf610839e6f11d4195f52",
			"/properties/intent_id/description":  "1dbc00bfc5f796aa99d19951cf34d5acc5dac7499d624956dc926901412351d1",
			"/properties/summary/description":    "2534f7d800eec244e1643aac4fd5fb71795688d78ad8e16edfbe3ad9bac65709",
		},
	},
	"report_finding": {
		description: "91675222f3d26a6fd171f6a7c1693c735d9985a7f93d16dcb77610f550d9655d",
		fields: map[string]string{
			"/properties/asset_ids/description":                                "1cf71f30e942c6acd5ad676e16031fdd22ada5e4226363021492d8eee4785a6b",
			"/properties/evidence/description":                                 "4b68357bd8aa3014558362eadf4affa02d14b350691bb29e2d2d4e8f67df1ff3",
			"/properties/evidence_hint_id/description":                         "8707cd5990842aff1bd05b3a66963df585a5eb254b1c8f532b1c6e0d2aa828c3",
			"/properties/intent_id/description":                                "ab3e22af02547f0a2552222139055c0d0dbc85a9c7a7daa90ac4dc3031701182",
			"/properties/name/description":                                     "f9d4d157ae40c7cbd8569265063fdb8cd4043f20c0a4a819958f36a261057f8a",
			"/properties/summary/description":                                  "f9a85d3fee807fa324e96999950c61d17e148c83410e81f9bf77e3a8c6e41168",
			"/properties/traffic_refs/description":                             "48d53e1a14cdbec3fef3ea34b549115bb9ec216e3e4ba9fe855136af7bfb1254",
			"/properties/traffic_refs/items/properties/note/description":       "d001c46cc621f04f0444356991a3e8285c52b5ff5203befda9ff05bfad1c6a78",
			"/properties/traffic_refs/items/properties/traffic_id/description": "68841d133d7c49865d964032a81f2996f718108333bca1c2c14612539fc81c70",
			"/properties/vulnclass/description":                                "2cbe44f7edc69649bb5412e20eeaf882ece1e565075143056a313e86dddbbc70",
		},
	},
	"search_all_worker_traces": {
		description: "a5198e6bdabe3c86b660a542fe30464ce6846cb522f29312f687ae84a340a0ac",
		fields: map[string]string{
			"/properties/limit/description": "3481f060b293c3c0d275e8b0c590a308e6d5469772eba804b56de351b6d83138",
			"/properties/q/description":     "0636c3744cae41de87952f9923e1f4d4480c944487ad82235595ab41df8eab06",
		},
	},
	"set_constraints": {
		description: "46a72ba15f4381fd8c8e2835ca5dee1c3054de81dde1209b5f913f5f8e68a41d",
		fields: map[string]string{
			"/properties/constraints/description": "0f0b5648d61cb0904a7f45f1d8fab76373625aaccd97e081f955673b67c08ae6",
			"/properties/text/description":        "a1eb9d02835e8faa0e2a16392e1793a7baf36fe9603f7e0e27547128a086f389",
			"/properties/type/description":        "3fc3cf3f2723f71a8ad3203cd45ff771ad3b5fa65e02c7a4e6a41b287e39fbd3",
		},
	},
	"set_goals": {
		description: "0115b98eabff22aea4710fe36255915c9de3b48cd451111da3074118ae83d1dd",
		fields: map[string]string{
			"/properties/goals/description":     "313a7790199bc1def7ec06c466605a5972ef609945b8b7a85fc6c026e3d24a24",
			"/properties/text/description":      "f27a11de5aec5ac4584834902518fd30f21e7022aa84d00d7274fb5f8470fbd0",
			"/properties/vulnclass/description": "a76b02b457da7c24c86b603951343b70e90e1bcbb9da48d269a4b20dfd8e0c02",
		},
	},
	"steer_work": {
		description: "a7a86be80c94c7e3525493136124939428413a3311b5283afa66256da6b5e135",
		fields: map[string]string{
			"/properties/intent_id/description": "36690860ededabca3915a3d81d41b5980419ccc826160cf19cef0cf866ec364b",
			"/properties/message/description":   "b5f1bedfc5792d82b96b8b941ac8d153ae120fabc1e874d490d49239191658c5",
		},
	},
}
