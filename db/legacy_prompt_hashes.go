package db

// legacyStockPromptHashes identifies unedited stock prompt bodies seeded by
// previous releases. Existing prompt versions remain available after migration.
var legacyStockPromptHashes = map[string]string{
	"_assistant": "a179c990bff1ab4e0a6f326945a1c44036ef4c8a54c7c5b635965f6cefdc9f0a",
	"auto":       "b2a71b82243607337b19f0642317fed88c144d536412d80eaaf90807f2905a1d",
	"goals":      "72a7a0159443b41dde29dfba8a3de6a2c8a15b273935495a7b88561fa708aa68",
	"mainagent":  "12bb69ee0886c4fb9beb936d882021a91104860f1da69a5e3eda3d9e345cd399",
	"pentest":    "a54bd5713b1c121d0f2ff3a72e0304820b1368c4b4b8159d5d651895663ac652",
	"planner":    "14e163f4a44da9fee20a214048a607adc3659b2025275569eb512532ca8c15d5",
	"reporter":   "8ba98208f6c5f2ccb103c88ef13239c4264733400c0e3dfec96d3a3572d78d39",
	"worker":     "80fbb1c610dd57e266c20b9145793339eee540b809f6db8f5336261b45879f08",
}
