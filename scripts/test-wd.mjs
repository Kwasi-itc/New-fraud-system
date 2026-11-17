async function test() {
  const q = `
  SELECT ?position ?positionLabel (COUNT(?person) AS ?count) WHERE {
    ?person wdt:P39 ?position .
    ?position wdt:P1001 wd:Q117 . # jurisdiction: Ghana
    SERVICE wikibase:label { bd:serviceParam wikibase:language "en". }
  }
  GROUP BY ?position ?positionLabel
  ORDER BY DESC(?count)
  LIMIT 25
  `;
  const url = `https://query.wikidata.org/sparql?query=${encodeURIComponent(q)}&format=json`;
  const res = await fetch(url, { headers: { "User-Agent": "Test/1.0" } });
  const data = await res.json();
  console.log(data.results.bindings.map(b => `${b.position.value} (${b.positionLabel.value}): ${b.count.value} people`));
}
test().catch(console.error);

