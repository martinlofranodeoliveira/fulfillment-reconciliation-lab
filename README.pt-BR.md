# Fulfillment Reconciliation Lab

**Versão de desenvolvimento 0.1.0 — não lançada.** Software original MIT da
[Lofrano Tecnologia](https://lofranotecnologia.com.br).
[English](README.md) · [Contrato](docs/contract.md) · [Projeto](docs/design.md)

Correlaciona quantidades sintéticas de pedidos/SKUs, eventos de reserva e snapshots
de remessas. Explica duplicações, conflitos, remessas divididas e fontes ausentes.
Não altera estoque, movimenta dinheiro, integra provedores reais nem utiliza
telemetria ou modelos/APIs em tempo de execução.

## Instalação local

Use **Go 1.27** já instalado e o diretório do código-fonte extraído. A CLI destina-se
a Linux/POSIX; não há suporte à CLI no Windows. A biblioteca Go usa código portátil
da biblioteca padrão. Não existem módulos externos para baixar. Prepare o
compilador separadamente dos comandos offline abaixo.

No diretório do projeto:

```sh
mkdir -p build/cache build/tmp build/gopath
export GOTOOLCHAIN=local GOENV=off GOPROXY=off
export GOCACHE="$PWD/build/cache" GOTMPDIR="$PWD/build/tmp" GOPATH="$PWD/build/gopath"
go build -o build/fulfillment-reconcile ./cmd/fulfillment-reconcile
build/fulfillment-reconcile help
```

Essas opções mantêm caches e temporários locais e desabilitam aquisição automática
de compiladores e resolução de módulos. Não há descoberta de workspace no HOME.

## Testes e demonstração offline determinística

```sh
go test ./...
go test -race -timeout=90s ./...
go run ./cmd/fulfillment-reconcile demo
go run ./cmd/fulfillment-reconcile demo --format=json
```

A demonstração inicia três servidores HTTP reais e limitados em `127.0.0.1`, busca
os três snapshots por `net/http`, verifica totais/classificações calculados
manualmente e demonstra cutoff divergente e falha HTTP 500 da fonte de remessas.
Clientes e servidores são fechados antes da saída. Não aparecem portas efêmeras
nem relógio ambiente no relatório. A saída JSON completa é determinística.

Início da saída textual:

```text
scenario=duplicates_conflict_partial_orphan
Fulfillment Reconciliation Lab 0.1.0 (development)
cutoffs_aligned=true pairs=4 matched=1 inconsistent=1 ambiguous=1 incomplete=0 orphan=1
ORDER/SKU CLASSIFICATION requested reserved shipped remaining
o1/s1 MATCHED 5 5 5 0 [EXACT_MATCH]
o2/s2 AMBIGUOUS_EVIDENCE 5 0 5 0 [CONFLICTING_RESERVATION_EVENT]
  Conflicting reservation variants excluded; totals are unquarantined subtotals.
o3/s3 INCONSISTENT_QUANTITIES 5 5 3 2 [SHIPMENT_SHORTFALL,UNFULFILLED_RESERVATION]
o999/s999 ORPHAN_EVIDENCE 0 0 2 -2 [ORPHAN_EVIDENCE_UNRECOGNIZED_ORDER]
```

Depois aparecem quatro apontamentos incompletos para cutoff divergente, três para
a fonte indisponível e contadores `orders=1 reservations=1 shipments=2`. Uma
asserção divergente encerra o comando com erro.

## CLI de arquivos

```sh
build/fulfillment-reconcile reconcile \
  --orders=testdata/fixtures/orders_aligned.json \
  --reservations=testdata/fixtures/reservations_aligned.json \
  --shipments=testdata/fixtures/shipments_aligned.json
```

Esses arquivos produzem três pares correspondentes. O primeiro soma remessas
`3+2=5`; a repetição idêntica da reserva conta uma vez. Exportação JSON para arquivo
**novo**:

```sh
build/fulfillment-reconcile reconcile \
  --orders=testdata/fixtures/orders_aligned.json \
  --reservations=testdata/fixtures/reservations_aligned.json \
  --shipments=testdata/fixtures/shipments_aligned.json \
  --format=json --out=build/relatorio.json
```

Arquivos de saída existentes são recusados. Omita uma flag de fonte para indicar
indisponibilidade. Um arquivo explicitamente informado que seja ilegível ou
inválido é erro, não ausência silenciosa. Informe pelo menos uma fonte. Apenas
arquivos regulares sem link simbólico são aceitos; diretórios e FIFOs são recusados.
O código de saída é zero para relatórios válidos, mesmo com divergências, e um para
erros operacionais, de validação ou de asserção. Diagnósticos não repetem caminhos
ou conteúdo bruto.

## Pequeno exemplo Go

```go
orders, err := strictjson.DecodeOrderSnapshot(reader)
if err != nil {
    return err
}
report, err := reconcile.Reconcile(orders, nil, nil)
if err != nil {
    return err
}
// Os apontamentos são incompletos: duas fontes estão indisponíveis.
_ = report
```

Importe `pkg/strictjson` e `pkg/reconcile` em
`github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab`.
A API pura também valida snapshots tipados, não modifica entradas e devolve
contêineres independentes. Não altere entradas concorrentemente durante a chamada.

## Decisões e limitações

- IDs exatos e sensíveis a maiúsculas são correlacionados por tuplas, sem busca
  aproximada nem colisões por separadores.
- Somente repetições idênticas de **event_id** são deduplicadas. Conflitos excluem
  todas as variantes dos totais e colocam todos os pares referenciados em
  quarentena. IDs de remessa e pares pedido/SKU devem ser únicos.
- Fonte ausente ou cutoff divergente prevalece como `INCOMPLETE_EVIDENCE`, mantendo
  razões de conflito. Com fontes alinhadas, ausência de registros de reserva ou
  remessa também é incompletude. Uma remessa parcial não vazia de 3 contra pedido e
  reserva de 5 é inconsistente, com diferença 2.
- Limites: 2 MiB/fonte, 5.000 registros/fonte, profundidade JSON 8, IDs ASCII de
  1–128 caracteres, unidades positivas int64 e 10.000 apontamentos. Overflow ou
  excesso de apontamentos falha integralmente, sem truncamento silencioso. A
  renderização da CLI limita-se a 16 MiB.
- HTTP aceita somente `http://127.0.0.1:<porta>/snapshot`, sem credenciais, consultas,
  fragmentos, compressão, proxies, redirecionamentos ou DNS. Não é um adaptador real.
- Horários UTC canônicos com precisão de segundos, versão explícita `0.1.0`, campos
  obrigatórios exatos, sem null, campos desconhecidos ou chaves JSON decodificadas
  duplicadas. Não há alegação de conformidade geral com padrões JSON/temporais.
- Snapshots fornecidos podem omitir fatos reais. Cutoffs iguais não certificam
  completude, estoque físico ou perda de mercadorias. Não há reparo ou reenvio.
- IDs de negócio ficam visíveis no relatório; escaping não anonimiza. Use apenas
  dados sintéticos. Uma falha de gravação pode deixar um relatório novo parcial;
  arquivos de entrada e saídas preexistentes não são sobrescritos.

## Projeto

Repositório: https://github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab

Veja [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md) e
[CHANGELOG.md](CHANGELOG.md). Contato geral pelo site oficial.
[Licença MIT](LICENSE), copyright 2026 Lofrano Tecnologia.
