package dependencies

func init() {
	SetupDependency("httpx", "", "import httpx\nimport encodings.idna\nimport socks")
	SetupDependency("requests", "", "import requests\nfrom netrc import netrc, NetrcParseError\nimport urllib3\nimport socket\nimport socks")
}
