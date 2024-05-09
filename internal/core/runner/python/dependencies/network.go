package dependencies

func init() {
	SetupDependency("httpx", "", "import httpx\nimport encodings.idna")
	SetupDependency("requests", "", "import requests\nfrom netrc import netrc, NetrcParseError\nimport urllib3\nimport socket")
}
