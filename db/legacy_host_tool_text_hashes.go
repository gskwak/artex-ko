package db

// legacyHostToolTextHashes covers untouched host tool defaults from the
// previous release: orchestration, platform, finding retest, and traffic tools.
var legacyHostToolTextHashes = map[string]legacyToolTextHash{
	"add_task_hint": {
		description: "4eb1f206d86556f2ccc0a394e5058250a602848a109f217cbd00bd3338b72b7e",
		fields: map[string]string{
			"/properties/asset_ids/description":                                                       "bb9e90d5045d52640c21337b6e0495ae145ff9bb4c20557a7c7fd58c268e8110",
			"/properties/hints/description":                                                           "64696993b1d2c72df0bddbff420a8401b6e417d5e4689404ae317f257f2578ca",
			"/properties/hints/items/properties/text/description":                                     "5d134b96d369b1a66fd6366629e78c5feed71760cf48b6156dcffe423c03c988",
			"/properties/hints/items/properties/traffic_refs/description":                             "adbe4c230b444dca6465305406ad65741e915e53830ddc7f134850d9830f5843",
			"/properties/hints/items/properties/traffic_refs/items/properties/note/description":       "d45da01df7568286565c3b9abfbd88f6927e72100bd0d6c8b59a1c3f342d36f0",
			"/properties/hints/items/properties/traffic_refs/items/properties/traffic_id/description": "8b0a5da66d7b8a5f781f189c7f49878759bc045c4f179014a3d30c06dc1fb425",
			"/properties/task_id/description":                                                         "d4a7cd32989ee46724d9580953a8b4d5ba93a32543bfe0bdaaefa2a0450e87e3",
			"/properties/text/description":                                                            "9c21fbae12af516970386d945f14e45cf30649e405faebd6dfa15dc744b75416",
			"/properties/traffic_refs/description":                                                    "adbe4c230b444dca6465305406ad65741e915e53830ddc7f134850d9830f5843",
			"/properties/traffic_refs/items/properties/note/description":                              "d45da01df7568286565c3b9abfbd88f6927e72100bd0d6c8b59a1c3f342d36f0",
			"/properties/traffic_refs/items/properties/traffic_id/description":                        "8b0a5da66d7b8a5f781f189c7f49878759bc045c4f179014a3d30c06dc1fb425",
		},
	},
	"bind_finding_traffic": {
		description: "e3ca4e11bf4064efe57ae573a90c73d7e9e8abf5185ae0c108c1984ae4d879af",
		fields: map[string]string{
			"/properties/finding_id/description":                               "23dde6b604235094d4055db2bcfe32647b4aed4695c200520f4ca2befe27fcb3",
			"/properties/traffic_refs/description":                             "adbe4c230b444dca6465305406ad65741e915e53830ddc7f134850d9830f5843",
			"/properties/traffic_refs/items/properties/note/description":       "d45da01df7568286565c3b9abfbd88f6927e72100bd0d6c8b59a1c3f342d36f0",
			"/properties/traffic_refs/items/properties/traffic_id/description": "8b0a5da66d7b8a5f781f189c7f49878759bc045c4f179014a3d30c06dc1fb425",
		},
	},
	"create_custom_tool": {
		description: "7bfdeabe156fc42becdab03079dab0f7d4d4aead08b0f782f537533121485a28",
		fields: map[string]string{
			"/properties/agents/description":      "e1608bfe908bfb95945f5d3e50d2e884e0531e191d61c4b3bcbb6d5fbc3847ac",
			"/properties/deferred/description":    "32631888de154d18119a57225bf034b4358ecdb45125a0e66fc61d6d50d4240d",
			"/properties/description/description": "2a38f72a090584cf40993e7390010ab16fff1f35aa56b3209c914719f71623c4",
			"/properties/enabled/description":     "66b628a80984404553813ad5e5035bd091c0feade19ce7dbb8ce02de86a288d1",
			"/properties/exec/description":        "38042ee8f136879d9ba616d138730dbac84ec486366855d72186361dbe3944f4",
			"/properties/key/description":         "33469f764bae96ade2412f6fa05420dbac3a20012c684d834bccc9fd5a879d28",
			"/properties/kind/description":        "d6fa1b6f50840e8c6e863622a559bda0c6517dc49fb31fea46532e2005e9138e",
			"/properties/schema/description":      "c933e6ec290e9c7f0d6a8d6d284c15ab7710e381bf264f9997903432dd19e99f",
		},
	},
	"create_mcp": {
		description: "e71f9d4f5647c8c57d7c0fbe1b86795060e3c31c76fd1d58122061d907b52595",
		fields: map[string]string{
			"/properties/args/description":     "20f8e76b5a2178f89671327919b8871d1a2f56232dfc3dec6767991d69bf8c9b",
			"/properties/command/description":  "cb819d44daa054b86d88992d666cfb4a2b6b0a87f40bca055a1d9cf056a70271",
			"/properties/enabled/description":  "66b628a80984404553813ad5e5035bd091c0feade19ce7dbb8ce02de86a288d1",
			"/properties/env/description":      "8a4b2cda45233c76fcad0dcfc2f326c246f6132cfd83bd9516eb4b286274a562",
			"/properties/insecure/description": "f2081c70e39a2db5e15ae2eea0b28cddb065eb467d5cea3bfda5fbd2684edace",
			"/properties/name/description":     "1ff6f7bcc11495b8b2f49e8a958bfe70fa607dc9ead7f0042877d506868f3b60",
			"/properties/url/description":      "5e1d09e15ada47ee73360a8722fb182f2275c1cb1773550e917e0d6a2268445f",
		},
	},
	"create_skill": {
		description: "40a5e60ab56cebb9f574dc9fb3bb6f2b418c8d8baa452ff441fbf32c1aa82d2e",
		fields: map[string]string{
			"/properties/description/description":  "93b371f7e41f3bb896a9d5dfa2d7ae8a6ae8f9b211c4a53dccd870b02f0e4444",
			"/properties/instructions/description": "a1862b9b140430d1646735b4fe278c7471bded74850e26dd2bd1af304f3ff760",
			"/properties/name/description":         "66ae63a62a167a5cbc9e9472dd37028d2202e3b46332bd06ec7460e4e6a4d190",
		},
	},
	"delete_assets_by_host": {
		description: "194b747997ca61c2b7a6929f61f7642dcc7e2ca24fb37c279fe6d6297ef0a1f0",
		fields: map[string]string{
			"/properties/host/description": "3cb85492dda11eeb1b6e14231a196c5b0f2bfb94b558bdde0e252d6895fc4ebe",
		},
	},
	"get_finding_retest_context": {
		description: "c7d224d35a8f484dab0bf72adea727236364b11f7cafe1674f60d81c7985d876",
	},
	"get_finding_traffic": {
		description: "9fbbf5633682ea7d6260cbbfcf20ffd0bf073d659123c8615cc79e753f38afd6",
		fields: map[string]string{
			"/properties/binding_id/description": "973c827c860fbf483b1216a3186504bd1a7d4a70dd16303459dab77a4c81ed78",
			"/properties/finding_id/description": "8c94b6a62777f99a31c53a6042920e60920f86e6d08f3032ed3bcf175f96019a",
			"/properties/side/description":       "9a576c60a84415010dd6425b6bd0dbe6d63f0556166550ad8aa28a6a618a9a3d",
		},
	},
	"get_task_graph": {
		description: "3f2787299dbfa6bb755a7653cb3cab0c00dece049f4c9a4203ebb105dff80ab8",
		fields: map[string]string{
			"/properties/task_id/description": "d4a7cd32989ee46724d9580953a8b4d5ba93a32543bfe0bdaaefa2a0450e87e3",
		},
	},
	"get_task_node_detail": {
		description: "a0dce2524d6ebc8dc5f94765419d398dd820811a1440169a6f406914a0c2804f",
		fields: map[string]string{
			"/properties/id/description":      "ca4e3a00d530cd6600b268af400680188f267b774b1bcf815e090d3d343ae161",
			"/properties/task_id/description": "d4a7cd32989ee46724d9580953a8b4d5ba93a32543bfe0bdaaefa2a0450e87e3",
		},
	},
	"get_task_worker_trace": {
		description: "5c5e3a70e3c8f650b094fcf1296b53de4f7f1763574b68a9142787b2adb77c28",
		fields: map[string]string{
			"/properties/intent_id/description": "106b8b92e4eb2dfa53b96d98d1293dce98001545962ea801a1bc2ec5ef7fea6d",
			"/properties/step_ids/description":  "1b20d0414f20422260e2877fdcfa1336d99258b13a03bd8874b39d9a3ce2ca5a",
			"/properties/task_id/description":   "d4a7cd32989ee46724d9580953a8b4d5ba93a32543bfe0bdaaefa2a0450e87e3",
		},
	},
	"list_llm_profiles": {
		description: "e4c1aac1216302ab483281f660ae6d528f6aa69fb76605d93fa8b27083aa98ef",
	},
	"list_task_findings": {
		description: "989043941aa39a3eff03f1f9d8088c392301a16dfe113d465301f08f7ac55ac0",
		fields: map[string]string{
			"/properties/task_id/description": "d4a7cd32989ee46724d9580953a8b4d5ba93a32543bfe0bdaaefa2a0450e87e3",
		},
	},
	"list_task_worker_traces": {
		description: "db57bc2c72a4b4e53a9b625fd1158b117521c4104f7d4a7c244b299fb7f15c04",
		fields: map[string]string{
			"/properties/task_id/description": "d4a7cd32989ee46724d9580953a8b4d5ba93a32543bfe0bdaaefa2a0450e87e3",
		},
	},
	"list_tasks": {
		description: "3d5515eb6136cead1b9dd4d3c11368051794ddc9a98c0012d883c7d9f59ac8c3",
	},
	"pause_task": {
		description: "d37d81dbb74321da470ca15e9b2cc6a04f85538f4b1611ef7e05f7f2e11bf6b1",
		fields: map[string]string{
			"/properties/task_id/description": "0361af052539ece90159abf148b30f5a6156efa71de29cfddc3b97346762f53e",
		},
	},
	"record_finding_retest_result": {
		description: "15bf1adec32a2b40e515263b5e8ab882b1328669687138dbbcd6cf74d606623f",
		fields: map[string]string{
			"/properties/evidence/description": "4a55b78d67cc65bff632c726b2007cc5069cf6a97829f71693fcfbfc51f5575f",
			"/properties/summary/description":  "8a2e8f617755d77cacd17aa5df0ec4c0a94f3de3b1db5b92893d43eb25319245",
		},
	},
	"search_task_worker_traces": {
		description: "3a52371e00c54c2287ecdc0cd824faed79e4e409281d2b7a991b7fb619efcb34",
		fields: map[string]string{
			"/properties/q/description":       "395bda79d8921f103638e5fa7173fe03e8037ad7d125a713ecb88a698b6b7967",
			"/properties/task_id/description": "d4a7cd32989ee46724d9580953a8b4d5ba93a32543bfe0bdaaefa2a0450e87e3",
		},
	},
	"spawn_task": {
		description: "d79dc8da3dbfe456ec7ca051ad6751ff8dbf3e2a4db00e0bd26bdbc2631471a6",
		fields: map[string]string{
			"/properties/description/description":            "914245fff7e40e827e0577098950ffee1ee51bd5faeb2893e92557128ab8cb33",
			"/properties/goal/description":                   "3481996ff755e096fd7d613a19888ecda7e1e901a39df0c1ae6571a825165e84",
			"/properties/llm_profile_id/description":         "668978d99a05d7a09aa71585b8809da24df66627bdb3bd14dfabaea55d35de2c",
			"/properties/parent_ref/description":             "bfcca5fe7981e1c11f3a59491624b032338d272e030cdb90e32c67f3ccf64b8c",
			"/properties/plan_heartbeat_seconds/description": "fd3dd17d13be616ae46cd7128d0e82ce5cfbb114b03a6c70ece0d9b5366911b1",
			"/properties/seed_first_intent/description":      "7232ffc78571d3e3cd2c4f9b113b17e303280297dcd0ee4772c2bdd83ac9aa75",
			"/properties/source_task_ids/description":        "cbe850bdb40c4a9c718b3a71c141a74dad4d46beec3d1584a8dc1eaf7a07ff37",
			"/properties/timeout_seconds/description":        "a78c60d717f858a738230bd33543f18c506970dd6a79204c1a223b442a469177",
		},
	},
	"traffic_blob": {
		description: "315f23469723e3c655c74e77f59e87608d750aebc2dce3b14619b24e1a3953c2",
		fields: map[string]string{
			"/properties/hash/description":   "eaffaaea1bb255e5c517ab9f5bb24474d569c668ac82d7497c31a416013bd7bc",
			"/properties/length/description": "875b7c53fe1dc14f1d44783326f3939cdc149a09e09240445bcf18aa634e0f14",
			"/properties/offset/description": "4a623576a532fa90192ada7f8fb464e94f3167c7d4d2c06dbb0056c9e9e962d2",
		},
	},
	"traffic_get": {
		description: "f530c0796b3822cde3bd18677d07b4189de5122dca6109ecbe8cd0e9a6e2f790",
		fields: map[string]string{
			"/properties/id/description": "a80cef3fe29715fa286da9bb76671be711b961f62a0e27e0835e874199018d24",
		},
	},
	"traffic_search": {
		description: "68cd3bfc0d83573b4696a2015b346f60d4950746d08b9658a1f930965d094b95",
		alternateDescriptions: []string{
			"5adfaf6a43f0c3d34d3b9d9994394f6dc2e3174d4789600d1b5abe44fa21617c",
		},
		fields: map[string]string{
			"/properties/body_contains/description": "349c5968c42a608c53655c27471ad171626a12cd5932daa5c229d87b3c4bbd9f",
			"/properties/contains/description":      "7bb3f7b749d77ac7f717a9a7accb1501d25fd585ba683b6e728310943ae68b11",
			"/properties/host/description":          "181695c7b1eda700e55859a31301b4720d2733e0c03f8b6b0c894eca5c468641",
			"/properties/limit/description":         "d3815d1bf3a8efa36d8f17a41b6f6a92f025378fe901ae8f4726fea9cd4ca3c6",
			"/properties/page/description":          "6dabc3ae1c34faaca2c8920f226df7a153f67fd314ea6773d1a6dc32eca6ff51",
		},
	},
	"update_custom_tool": {
		description: "f44f48e618598dd2a99e2a2e2cf355da52751dc5f74734616772158c13a9e50c",
		fields: map[string]string{
			"/properties/agents/description":      "e1608bfe908bfb95945f5d3e50d2e884e0531e191d61c4b3bcbb6d5fbc3847ac",
			"/properties/deferred/description":    "32631888de154d18119a57225bf034b4358ecdb45125a0e66fc61d6d50d4240d",
			"/properties/description/description": "2a38f72a090584cf40993e7390010ab16fff1f35aa56b3209c914719f71623c4",
			"/properties/enabled/description":     "66b628a80984404553813ad5e5035bd091c0feade19ce7dbb8ce02de86a288d1",
			"/properties/exec/description":        "38042ee8f136879d9ba616d138730dbac84ec486366855d72186361dbe3944f4",
			"/properties/key/description":         "57c75ad0d460bc8ded09114b39bbce12fcd627ca338a6786758ec91b5a4b42e3",
			"/properties/kind/description":        "d6fa1b6f50840e8c6e863622a559bda0c6517dc49fb31fea46532e2005e9138e",
			"/properties/schema/description":      "c933e6ec290e9c7f0d6a8d6d284c15ab7710e381bf264f9997903432dd19e99f",
		},
	},
	"update_finding_report": {
		description: "243d0fbc8e96f7bb76ac8e1e766baa1fec0c61c458d0689bcc50b5624bdeca9f",
		fields: map[string]string{
			"/properties/evidence_version/description": "f91b509fc2ae08447539e75d1c9b00ddfa2b5d61eac2739dca3c3565ce0019f3",
			"/properties/finding_id/description":       "25082c5bcd8bcc3a7cc2b1ba2b9177416eea459a4d4514ef08bef37887db78f5",
			"/properties/report/description":           "9116d3ac9a1591bd6a124b6c90032f6a2d396d6022742555687e56f833f4d089",
		},
	},
	"update_mcp": {
		description: "f045b5bd141083f8837c0958af8fdcce92b120a164b407a62d77063926a61f10",
		fields: map[string]string{
			"/properties/args/description":     "20f8e76b5a2178f89671327919b8871d1a2f56232dfc3dec6767991d69bf8c9b",
			"/properties/command/description":  "cb819d44daa054b86d88992d666cfb4a2b6b0a87f40bca055a1d9cf056a70271",
			"/properties/enabled/description":  "66b628a80984404553813ad5e5035bd091c0feade19ce7dbb8ce02de86a288d1",
			"/properties/env/description":      "8a4b2cda45233c76fcad0dcfc2f326c246f6132cfd83bd9516eb4b286274a562",
			"/properties/id/description":       "dfbefa1576305148a5af2063a4994d15a7896624e639e945b839fb9e1704ca37",
			"/properties/insecure/description": "f2081c70e39a2db5e15ae2eea0b28cddb065eb467d5cea3bfda5fbd2684edace",
			"/properties/name/description":     "1ff6f7bcc11495b8b2f49e8a958bfe70fa607dc9ead7f0042877d506868f3b60",
			"/properties/url/description":      "5e1d09e15ada47ee73360a8722fb182f2275c1cb1773550e917e0d6a2268445f",
		},
	},
	"update_skill_file": {
		description: "c9c09017e8049d928603dbbfbfc66078d64ef5889368d4f96eb954d86fb6d994",
		fields: map[string]string{
			"/properties/content/description": "b3e4ee5381ed986532f7c6b580e2a26d9d5b4c744757ce65891ae6672fb04ae8",
			"/properties/file/description":    "57aae6e9fe8d433145a091ecbced927608d9fe3e703514d7a36274005e617b33",
			"/properties/name/description":    "75ff03de86ae228ba58fecf7007b1b55f8acbb296b737df7821628bfd4b5545b",
		},
	},
}
