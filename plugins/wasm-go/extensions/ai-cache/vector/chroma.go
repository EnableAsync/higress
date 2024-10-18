package vector

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/alibaba/higress/plugins/wasm-go/pkg/wrapper"
)

type chromaProviderInitializer struct{}

const chromaThreshold = 1000

func (c *chromaProviderInitializer) ValidateConfig(config ProviderConfig) error {
	if len(config.collectionID) == 0 {
		return errors.New("[Chroma] collectionID is required")
	}
	if len(config.serviceName) == 0 {
		return errors.New("[Chroma] serviceName is required")
	}
	return nil
}

func (c *chromaProviderInitializer) CreateProvider(config ProviderConfig) (Provider, error) {
	return &ChromaProvider{
		config: config,
		client: wrapper.NewClusterClient(wrapper.DnsCluster{
			ServiceName: config.serviceName,
			Port:        config.servicePort,
			Domain:      config.serviceDomain,
		}),
	}, nil
}

type ChromaProvider struct {
	config ProviderConfig
	client wrapper.HttpClient
}

func (c *ChromaProvider) GetProviderType() string {
	return providerTypeChroma
}

func (d *ChromaProvider) GetSimilarityThreshold() float64 {
	return chromaThreshold
}

func (d *ChromaProvider) QueryEmbedding(
	emb []float64,
	ctx wrapper.HttpContext,
	log wrapper.Log,
	callback func(results []QueryResult, ctx wrapper.HttpContext, log wrapper.Log, err error)) error {
	// 最少需要填写的参数为 collection_id, embeddings 和 ids
	// 下面是一个例子
	// {
	// 	"where": {}, // 用于 metadata 过滤，可选参数
	// 	"where_document": {}, // 用于 document 过滤，可选参数
	// 	"query_embeddings": [
	// 	  [1.1, 2.3, 3.2]
	// 	],
	// 	"limit": 5,
	// 	"include": [
	// 	  "metadatas", // 可选
	// 	  "documents", // 如果需要答案则需要
	// 	  "distances"
	// 	]
	// }

	requestBody, err := json.Marshal(chromaQueryRequest{
		QueryEmbeddings: []chromaEmbedding{emb},
		Limit:           d.config.topK,
		Include:         []string{"distances", "documents"},
	})

	if err != nil {
		log.Errorf("[Chroma] Failed to marshal query embedding request body: %v", err)
		return err
	}

	d.client.Post(
		fmt.Sprintf("/api/v1/collections/%s/query", d.config.collectionID),
		[][2]string{
			{"Content-Type", "application/json"},
		},
		requestBody,
		func(statusCode int, responseHeaders http.Header, responseBody []byte) {
			log.Infof("[Chroma] Query embedding response: %d, %s", statusCode, responseBody)
			results, err := d.parseQueryResponse(responseBody, log)
			if err != nil {
				err = fmt.Errorf("[Chroma] Failed to parse query response: %v", err)
			}
			callback(results, ctx, log, err)
		},
		d.config.timeout,
	)
	return errors.New("[Chroma] QueryEmbedding Not implemented")
}

func (d *ChromaProvider) UploadAnswerAndEmbedding(
	queryString string,
	queryEmb []float64,
	queryAnswer string,
	ctx wrapper.HttpContext,
	log wrapper.Log,
	callback func(ctx wrapper.HttpContext, log wrapper.Log, err error)) error {
	// 最少需要填写的参数为 collection_id, embeddings 和 ids
	// 下面是一个例子
	// {
	// 	"embeddings": [
	// 		  [1.1, 2.3, 3.2]
	// 	],
	// 	"ids": [
	// 	  "你吃了吗？"
	// 	],
	//  "documents": [
	//    "我吃了。"
	//  ]
	// }
	// 如果要添加 answer，则按照以下例子
	// {
	// 	"embeddings": [
	// 	  [1.1, 2.3, 3.2]
	// 	],
	// 	"documents": [
	// 	  "answer1"
	// 	],
	// 	"ids": [
	// 	  "id1"
	// 	]
	// }
	requestBody, err := json.Marshal(chromaInsertRequest{
		Embeddings: []chromaEmbedding{queryEmb},
		IDs:        []string{queryString}, // queryString 指的是用户查询的问题
		Documents:  []string{queryAnswer}, // queryAnswer 指的是用户查询的问题的答案
	})

	if err != nil {
		log.Errorf("[Chroma] Failed to marshal upload embedding request body: %v", err)
		return err
	}

	err = d.client.Post(
		fmt.Sprintf("/api/v1/collections/%s/add", d.config.collectionID),
		[][2]string{
			{"Content-Type", "application/json"},
		},
		requestBody,
		func(statusCode int, responseHeaders http.Header, responseBody []byte) {
			log.Infof("[Chroma] statusCode:%d, responseBody:%s", statusCode, string(responseBody))
			callback(ctx, log, err)
		},
		d.config.timeout,
	)
	return err
}

type chromaEmbedding []float64
type chromaMetadataMap map[string]string
type chromaInsertRequest struct {
	Embeddings []chromaEmbedding   `json:"embeddings"`
	Metadatas  []chromaMetadataMap `json:"metadatas,omitempty"` // Optional metadata map array
	Documents  []string            `json:"documents,omitempty"` // Optional document array
	IDs        []string            `json:"ids"`
}

type chromaQueryRequest struct {
	Where           map[string]string `json:"where,omitempty"`          // Optional where filter
	WhereDocument   map[string]string `json:"where_document,omitempty"` // Optional where_document filter
	QueryEmbeddings []chromaEmbedding `json:"query_embeddings"`
	Limit           int               `json:"limit"`
	Include         []string          `json:"include"`
}

type chromaQueryResponse struct {
	Ids        [][]string          `json:"ids"`                  // 第一维是 batch query，第二维是查询到的多个 ids
	Distances  [][]float64         `json:"distances,omitempty"`  // 与 Ids 一一对应
	Metadatas  []chromaMetadataMap `json:"metadatas,omitempty"`  // Optional, can be null
	Embeddings []chromaEmbedding   `json:"embeddings,omitempty"` // Optional, can be null
	Documents  [][]string          `json:"documents,omitempty"`  // 与 Ids 一一对应
	Uris       []string            `json:"uris,omitempty"`       // Optional, can be null
	Data       []interface{}       `json:"data,omitempty"`       // Optional, can be null
	Included   []string            `json:"included"`
}

func (d *ChromaProvider) parseQueryResponse(responseBody []byte, log wrapper.Log) ([]QueryResult, error) {
	var queryResp chromaQueryResponse
	err := json.Unmarshal(responseBody, &queryResp)
	if err != nil {
		return nil, err
	}

	log.Infof("[Chroma] queryResp Ids len: %d", len(queryResp.Ids))
	if len(queryResp.Ids) == 1 && len(queryResp.Ids[0]) == 0 {
		return nil, errors.New("no query results found in response")
	}
	results := make([]QueryResult, 0, len(queryResp.Ids[0]))
	for i := range queryResp.Ids[0] {
		result := QueryResult{
			Text:   queryResp.Documents[0][i],
			Score:  queryResp.Distances[0][i],
			Answer: queryResp.Documents[0][i],
		}
		results = append(results, result)
	}
	return results, nil
}
