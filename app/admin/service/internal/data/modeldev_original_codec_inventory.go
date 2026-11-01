package data

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"github.com/zhangzhe-ctrl/ani-modeldev-service/contract/cpup01"
	"go-wind-admin/app/admin/service/internal/data/ent/modeldevacceptance"
	appViewer "go-wind-admin/pkg/entgo/viewer"
)

type OriginalCodecFailure struct{ Source, ExecutionID, SpecSHA256, Guard string }
type OriginalCodecAudit struct {
	MDTotal, MDPass, GovTotal, GovPass int
	Failures                           []OriginalCodecFailure
}
type originalCodecIdentity struct {
	ID                         uint32
	Operation, Execution, Spec string
}

// Read-only scope from the original inventory; this does not enlarge the four-row write scope.
var originalMDCodecScope = []originalCodecIdentity{
	{0, "de723ecd-9627-4336-a9db-58d96da9d052", "0374fd04-2b3c-4a7d-ac8c-e93ccaedc3ce", "fd1ff3da0a09dd66fc49bc2370602e1e346bd059eab4df1e97b057108f71bf0e"},
	{0, "8cb22cc3-04cf-4be8-b781-6c4781ef445f", "111952ee-9d72-46d9-9981-c2a1ead73289", "d0a5d82fc032f3d6b5571a0c13817611efdb8f361520684bce98d93f33c01fbf"},
	{0, "125097d9-270e-4448-a339-fb050a4acd02", "165037e3-49fd-4889-9018-cf9d95600b29", "07059214e8e4fefc4907188833bd6fb998d2fb64d224224b342474bb46771772"},
	{0, "8ac3f398-a3c2-48e4-89e3-087c77c827e3", "16d56223-4081-47ae-9ecd-61eb9104c6e7", "fc578f382d123a416a691d13574d05eadca5faf090bda1453fb8f89fb5b36998"},
	{0, "0de465e0-2bae-4cc7-9f33-2f438b75c759", "196d30c0-e63d-4f73-a073-fd0f49e5401d", "5393a1d447cf39f2d07c99a5bf899ac2cf6f1a24e2225e108ce1289dbea4b856"},
	{0, "ec266a51-6708-401c-a735-a4887d26185e", "1cce950b-d6e9-453e-aa11-fffd8846224b", "2385186a840df5cddef1778071703aee310ee6fef9a27bf381120429d0560893"},
	{0, "53358b7b-843c-47bf-886b-c9f0efcb34b3", "1d8e112d-3612-4080-8dab-a24c383da879", "5c359868965b4e41485307900eb09344e28b0f50a08737d317b104322f1b600c"},
	{0, "8e54db68-2b16-44ad-89ba-7eb82dee504c", "2454cb50-d7f6-4efb-a1fd-f8d289631ba2", "6adce43e755c7adee4bc9782a0a4ff0db009edb8ae450bfa479d762307430828"},
	{0, "adfd7e06-a2f5-4cce-b97c-4de767256ac9", "2a7b1169-7466-4561-85bf-faceee50b027", "50270369b79cf434ca36c667c1599671ba89c07b09f177cb1e3ecfadf2a21fdc"},
	{0, "3ff8fcf7-5ff3-4a18-b2ad-32cc8625043f", "2ab49378-9784-4ea9-a06a-b84ad255f238", "9a5598f800ac199ae2784b6518cfc7120a975841b15cdb8b24411dc071744933"},
	{0, "e62ab0c9-f864-439d-a164-2383fa8e954f", "2abb5610-1bb7-4ca1-8bb1-29a691add6f3", "fd8661cc8f7f7db5a707f161f148a093f9fa137fc77bc2bfc3001e6d4df586c2"},
	{0, "ed6e99a5-c84b-4e40-b307-f42208b2138a", "34b225da-9028-4793-84db-4501c708076e", "bf9aa4f53cb4f32ccc00db314e0f38d174900e385b06323e9ad1703cfb685a03"},
	{0, "3608031b-9fb6-4b7a-9bc0-bab70501d464", "3aca77ad-0de5-482d-bb9a-2427a19ce60e", "2fc76782c1a4a60a44054fbbb79c1838b3af6f3e572fc8781d54c520980091bb"},
	{0, "5dc5dab0-fd65-4784-a987-30f9ac8c646f", "3f91e6cb-0348-4808-af67-b86c96806116", "01f7ab4e37bb1cc2ef89d54faf4dea9b5cfa42411f7c4591d38748bccf5b66f5"},
	{0, "6e189b3c-a168-4761-9beb-963902c33877", "43d9a893-ff56-41ca-b36c-ea77ef546d0c", "d85e2cfecf0346e207a848d2b76447ccc602afa050730936b2b1f56fac429cc2"},
	{0, "644dfe94-5311-4cc5-9a33-c6296de0fecb", "4534a7de-480c-4a8f-ae76-e647cf7c158c", "637373fc4213532e0724392564d9c2fb437575b5593eb88760f60b303ba93624"},
	{0, "44315719-39ee-48b1-add0-724ae1f9a82b", "486a9036-7dc9-477d-af9b-06e00920c6ef", "7fbce3d072d41964bcad25ef58a7b1899fd6dfe49727cd60f0adfaf822c21617"},
	{0, "3d0aaae2-705f-4786-a307-520d701d5a2d", "4a1a92f7-4d94-439d-a8d7-1833f38c7dbc", "801ce15e644881d9c92699358b28aa15edd1c7a5ba342761d19ef34c88e2ae2d"},
	{0, "5febd4b1-cb81-42a3-acd9-9f9d98a8f347", "4c06e7ed-6f0c-46b8-adbf-c060f0233dae", "9780063dc164b7417c252bbdd033b1d1b9a56e13fbceb1faa0b9be4780dc13fe"},
	{0, "6c36eb05-b27e-4564-b8b6-b95f8da8ccc4", "4de6d08d-b41e-4940-b777-a7b9d6d0370b", "cee57cd5d2526f855456b334d44c1166a99029dc9f1ec9233c8fe428e5efc25b"},
	{0, "b8a8d332-cc4b-4816-9a1a-7a347b2fa204", "500063f2-2f5d-4e82-9208-1e3ad92dfee5", "1519e7274c5650b52396f8531aa7fc611f386b3de141d95b7cde4370d950d43a"},
	{0, "badf5481-6202-4619-8173-ece940ff972c", "5953c2e1-107e-4d46-af6d-4c718c7fb10c", "0208188cf2da722447cf84fbaa1459142a1fc4300f57360700971ec4c1d31208"},
	{0, "52ab36df-8e61-41cf-9519-b76aee82ebbd", "5f0b866e-31d1-4b44-8a7c-5cc900767e00", "bd1fe7a7524ca89f61702eeb28a4ed3b74a8e39b0da30512dfeb3b80013eb12c"},
	{0, "22545b38-0dfb-46dd-909e-c4ad2fcab5a1", "6c603151-edf3-4785-a3b0-00aaa6607c1d", "d441887fb9c612b753545bdb3e6c803c43cd6b6f7ee7cbace6a4aaa51a0573e2"},
	{0, "0d8b2a3c-86e3-448c-9156-d7f59e40b11c", "6df02902-13bf-4b35-9f74-93dae719888c", "cc1c3481a3b2a84c429a64554a893fe1cfbade64ecc236aeb55ddbc811440b6d"},
	{0, "711b9a7d-fed4-4fa4-b7e7-3d719c1a64ac", "6ea21854-9dbd-44e1-92a0-ee9e0613a3a5", "d38ac0280cbf01dd1817433bd69c60f038a3074f26b41709bc78ecb7f5968f52"},
	{0, "19394d9c-1050-46ef-b931-5ef3123f12a0", "7646dbe7-bcbb-4760-926a-580c9ca6934a", "749e57e5eded5776703335b88e40adebca3884a6ec24a22a54eec891afc044d4"},
	{0, "9fea0692-dd43-4b3d-ad29-34db16b3cb93", "78ef7040-bd1c-4bff-b974-b9c4fa379dda", "15da1e8633f1bb905a097f334a80f9fdf2871a9be12ac4333ed91b55e370fe86"},
	{0, "f16307c1-1dec-48d1-9442-8f5a1bdc1bda", "8540d576-fe69-4937-a703-6d9e67fb093d", "f34a2f7f0e5a22cb1bd6b9cae4856fae5ca04df60597a890422e3f1e20759e15"},
	{0, "6a14b78a-b4f3-4a10-9e94-47e61264a372", "87181865-8f35-4983-94d7-92b9afc9b9cf", "0a98065ac18418aa441f85ce339fa1c5e5f282e839aa891b4ba203dfd468af06"},
	{0, "ac293c97-dedc-44e8-b1b9-cddcdc1b499c", "97f2f3d1-c20e-4f35-91d4-2e860a333962", "eda60e9bf669a9cfd74db2c409ba594afca37e0000a719c60bd5e5efab4430de"},
	{0, "06ecd780-450f-4c29-a5ef-cc772c007b4b", "abe39ad8-c017-416b-8041-6f097e8fd50a", "face714e1e21d1093bf1eaba967a3a0ceb6875f70080be23703ba5668ff21cfa"},
	{0, "e0f69f38-801f-4dac-9d74-ed0233951be1", "b857f693-8c61-4c7e-bc11-805679f06b26", "f7db29f26c76ea24f166d6a0519ee76b4afc0d063fee8483b882533bc7577524"},
	{0, "44e43514-c3a8-4d15-9d8d-1070ab87f751", "b911aeeb-6a40-4738-aab5-78c7fdfdf21f", "91da80771feb675f99a6cd5c6b03d32f87015bba058fc1e2340d9da9fe33301a"},
	{0, "1882bd1f-8e53-4a9b-8450-a91015664837", "b95620b8-9d13-416a-aeab-8576833a7e8c", "fcabc31eb52d1911f4e58831ffc0f6e22f6a82a119862414300fa82c5096a4b4"},
	{0, "86721b51-db3d-42f0-9ba2-7f208d461286", "ba7440e0-2f4e-49fa-8b90-ce25bc73474c", "0664b59380117b1aced18e0fb4587eaf745c6d8a35f33867b58de6e2fbfcc175"},
	{0, "da8ad421-b3bc-43ab-9f40-40a8181eb87d", "bbc8e3ac-b712-4507-8846-9b09e13717de", "354b89ea4939a9f9ed4d97c401e2c84f71c11228fd429404f60b680f96b3adc7"},
	{0, "37514867-eda8-46a2-ae96-d4bcf7551d0b", "bc870703-9ffd-47e8-871c-893e676c5ce1", "69f648c61eb72e79b4b7add6057e9f1acb24402c13598eb0206eab96c87c9c5f"},
	{0, "f47a2dda-2e3a-4ae7-a33c-7b6c99bc4fd8", "c01f0bd0-32d8-411e-bb0c-a52d51833ce1", "442d75b4a75514a8419dcebcad6e36b93ccdd1b9d0f8566dc580e75464fcde36"},
	{0, "fda33eaa-0997-4655-b53c-c32d6652a44e", "c0fa3739-d279-4bb3-85be-0f74b8d4773a", "9251e87631c02b1b0f33803644240f1a0c5c3bdcf8e50602e0df3ed82cad5906"},
	{0, "d292dbd2-e231-4505-b3c1-18d7cd2e12a8", "c436b386-b875-4365-be8c-39398228e137", "a36876b433777854c483bf80ae57fd512e155d35e2e9f6b74e6c5f4481bebc80"},
	{0, "232c7dd6-9efc-4527-8bc0-ec717c7e418c", "c8b9dada-caaf-451b-8aeb-809396f0c075", "5ddd6902dbd3432ae176c178532966ee295a1fc1888acdcc6b5e15d73df41a1c"},
	{0, "5d5b9742-be47-4153-b8a6-39a5845b526f", "cb9b2b16-ebbe-4690-a1f4-0bbc6651174b", "bd93ef1993da034c6f745d004ea346ca93a5fb036d842e67070d6bb3387b72ca"},
	{0, "b665cc75-73b4-4434-b5bb-cd35a8d0eb5e", "ce31c3da-f948-4dcf-b01c-8943b0f36d7f", "61474ed6c7588ce8c450e79cef239ce94f087d67e3d1cd94139249ee74b110ba"},
	{0, "f33bb9e8-5247-4405-a137-2e62f9e718d4", "dc612759-06fc-4b4a-b79c-b812f4b5ee5b", "7fceeb89cc1bf5ff7147c2d072f45c0ab5e4b10dcefcbcffd65a5571a8e4380b"},
	{0, "8c42a0b2-e603-4e85-bdd1-30f649d7c6cc", "e94344af-a825-4ba2-9fa1-838638cc4d5f", "a7706257d2a8afca898447799b7ac35ba6db1bb6bb1f8ceb9c81dbbd7e641639"},
	{0, "b448bc04-fe3f-4322-9c16-f08e7c8090db", "ec57bafc-56bd-4f7e-8f65-45527aa4d0e3", "2472153d80484cd6ee777afa9aebdc5f1b5275d861ef294b6e1fca3bb822938d"},
	{0, "1bc185ad-0977-4b3f-bb1c-404bc37e94fa", "eca4d47b-5911-4f0f-8b45-89c1be775925", "c149fba74453b130803396f16de66fb9ca21b8c975c66a388d5619943e50fe7c"},
	{0, "49a62211-703b-4adf-abae-70cf2af78e7e", "f2168fbf-d7c9-4e96-8cc6-784bb1546083", "60df838e3e66d376e7c8c8de51c9ef630aa6aaa6b627823c9867f1da2fdca232"},
	{0, "b4c67631-809a-4b3a-a417-3f5ad6588234", "fd413b1b-58f5-440c-b95c-20be5062d272", "2cf32acd2c64f3e5ba39044bf7fe57f2af8967f6546af463bb80af9686df0243"},
}
var originalGovCodecScope = []originalCodecIdentity{
	{34, "644dfe94-5311-4cc5-9a33-c6296de0fecb", "4534a7de-480c-4a8f-ae76-e647cf7c158c", "637373fc4213532e0724392564d9c2fb437575b5593eb88760f60b303ba93624"},
	{35, "badf5481-6202-4619-8173-ece940ff972c", "5953c2e1-107e-4d46-af6d-4c718c7fb10c", "0208188cf2da722447cf84fbaa1459142a1fc4300f57360700971ec4c1d31208"},
	{36, "52ab36df-8e61-41cf-9519-b76aee82ebbd", "5f0b866e-31d1-4b44-8a7c-5cc900767e00", "bd1fe7a7524ca89f61702eeb28a4ed3b74a8e39b0da30512dfeb3b80013eb12c"},
	{37, "b448bc04-fe3f-4322-9c16-f08e7c8090db", "ec57bafc-56bd-4f7e-8f65-45527aa4d0e3", "2472153d80484cd6ee777afa9aebdc5f1b5275d861ef294b6e1fca3bb822938d"},
	{38, "fda33eaa-0997-4655-b53c-c32d6652a44e", "c0fa3739-d279-4bb3-85be-0f74b8d4773a", "9251e87631c02b1b0f33803644240f1a0c5c3bdcf8e50602e0df3ed82cad5906"},
	{39, "6a14b78a-b4f3-4a10-9e94-47e61264a372", "87181865-8f35-4983-94d7-92b9afc9b9cf", "0a98065ac18418aa441f85ce339fa1c5e5f282e839aa891b4ba203dfd468af06"},
	{40, "9fea0692-dd43-4b3d-ad29-34db16b3cb93", "78ef7040-bd1c-4bff-b974-b9c4fa379dda", "15da1e8633f1bb905a097f334a80f9fdf2871a9be12ac4333ed91b55e370fe86"},
	{41, "37514867-eda8-46a2-ae96-d4bcf7551d0b", "bc870703-9ffd-47e8-871c-893e676c5ce1", "69f648c61eb72e79b4b7add6057e9f1acb24402c13598eb0206eab96c87c9c5f"},
	{42, "3608031b-9fb6-4b7a-9bc0-bab70501d464", "3aca77ad-0de5-482d-bb9a-2427a19ce60e", "2fc76782c1a4a60a44054fbbb79c1838b3af6f3e572fc8781d54c520980091bb"},
	{43, "b4c67631-809a-4b3a-a417-3f5ad6588234", "fd413b1b-58f5-440c-b95c-20be5062d272", "2cf32acd2c64f3e5ba39044bf7fe57f2af8967f6546af463bb80af9686df0243"},
	{44, "5dc5dab0-fd65-4784-a987-30f9ac8c646f", "3f91e6cb-0348-4808-af67-b86c96806116", "01f7ab4e37bb1cc2ef89d54faf4dea9b5cfa42411f7c4591d38748bccf5b66f5"},
	{45, "8e54db68-2b16-44ad-89ba-7eb82dee504c", "2454cb50-d7f6-4efb-a1fd-f8d289631ba2", "6adce43e755c7adee4bc9782a0a4ff0db009edb8ae450bfa479d762307430828"},
	{46, "0de465e0-2bae-4cc7-9f33-2f438b75c759", "196d30c0-e63d-4f73-a073-fd0f49e5401d", "5393a1d447cf39f2d07c99a5bf899ac2cf6f1a24e2225e108ce1289dbea4b856"},
	{47, "8c42a0b2-e603-4e85-bdd1-30f649d7c6cc", "e94344af-a825-4ba2-9fa1-838638cc4d5f", "a7706257d2a8afca898447799b7ac35ba6db1bb6bb1f8ceb9c81dbbd7e641639"},
	{48, "f47a2dda-2e3a-4ae7-a33c-7b6c99bc4fd8", "c01f0bd0-32d8-411e-bb0c-a52d51833ce1", "442d75b4a75514a8419dcebcad6e36b93ccdd1b9d0f8566dc580e75464fcde36"},
	{49, "125097d9-270e-4448-a339-fb050a4acd02", "165037e3-49fd-4889-9018-cf9d95600b29", "07059214e8e4fefc4907188833bd6fb998d2fb64d224224b342474bb46771772"},
	{50, "e62ab0c9-f864-439d-a164-2383fa8e954f", "2abb5610-1bb7-4ca1-8bb1-29a691add6f3", "fd8661cc8f7f7db5a707f161f148a093f9fa137fc77bc2bfc3001e6d4df586c2"},
	{51, "53358b7b-843c-47bf-886b-c9f0efcb34b3", "1d8e112d-3612-4080-8dab-a24c383da879", "5c359868965b4e41485307900eb09344e28b0f50a08737d317b104322f1b600c"},
	{52, "44e43514-c3a8-4d15-9d8d-1070ab87f751", "b911aeeb-6a40-4738-aab5-78c7fdfdf21f", "91da80771feb675f99a6cd5c6b03d32f87015bba058fc1e2340d9da9fe33301a"},
	{53, "8cb22cc3-04cf-4be8-b781-6c4781ef445f", "111952ee-9d72-46d9-9981-c2a1ead73289", "d0a5d82fc032f3d6b5571a0c13817611efdb8f361520684bce98d93f33c01fbf"},
	{54, "ed6e99a5-c84b-4e40-b307-f42208b2138a", "34b225da-9028-4793-84db-4501c708076e", "bf9aa4f53cb4f32ccc00db314e0f38d174900e385b06323e9ad1703cfb685a03"},
	{55, "13bd316b-0b85-4c8e-a005-698eb9ffdd5e", "27cf73a3-4107-4c07-86f7-862ce9fb413d", "cb9831ca80e7df95cf1ca3746db4ff53b623b51671efcf34230275235e0c1690"},
	{56, "3d0aaae2-705f-4786-a307-520d701d5a2d", "4a1a92f7-4d94-439d-a8d7-1833f38c7dbc", "801ce15e644881d9c92699358b28aa15edd1c7a5ba342761d19ef34c88e2ae2d"},
	{57, "cd2ee5e3-e4fb-4515-99f8-33b3da892a8f", "c9b470e0-d9cb-4989-a7f2-d1fbfbe72ab9", "9883ac49d909b2809e78002db7d47d564caa88f84bd29ba14d6248a34e8bb096"},
	{58, "f16307c1-1dec-48d1-9442-8f5a1bdc1bda", "8540d576-fe69-4937-a703-6d9e67fb093d", "f34a2f7f0e5a22cb1bd6b9cae4856fae5ca04df60597a890422e3f1e20759e15"},
	{59, "ff5fc1f4-d9a8-4c6a-8c47-5b1d4ef06f05", "91e351e7-102e-4807-b0d2-9236c1426da9", "e75a332cef7521f278df8fc78df0b3f869d8499f96d188fd3b8a915fe02ec829"},
	{60, "25b88cf7-bccb-4fac-8c9a-6b3ff91477ee", "f148135c-640f-47ed-b9b1-4b039d4709d1", "85fa8793e5ff44c8c611316129f344a36124e027f03cc71bf797d89fa1d40a45"},
	{61, "44315719-39ee-48b1-add0-724ae1f9a82b", "486a9036-7dc9-477d-af9b-06e00920c6ef", "7fbce3d072d41964bcad25ef58a7b1899fd6dfe49727cd60f0adfaf822c21617"},
	{62, "232c7dd6-9efc-4527-8bc0-ec717c7e418c", "c8b9dada-caaf-451b-8aeb-809396f0c075", "5ddd6902dbd3432ae176c178532966ee295a1fc1888acdcc6b5e15d73df41a1c"},
	{63, "f33bb9e8-5247-4405-a137-2e62f9e718d4", "dc612759-06fc-4b4a-b79c-b812f4b5ee5b", "7fceeb89cc1bf5ff7147c2d072f45c0ab5e4b10dcefcbcffd65a5571a8e4380b"},
	{64, "ec266a51-6708-401c-a735-a4887d26185e", "1cce950b-d6e9-453e-aa11-fffd8846224b", "2385186a840df5cddef1778071703aee310ee6fef9a27bf381120429d0560893"},
	{65, "e0f69f38-801f-4dac-9d74-ed0233951be1", "b857f693-8c61-4c7e-bc11-805679f06b26", "f7db29f26c76ea24f166d6a0519ee76b4afc0d063fee8483b882533bc7577524"},
	{66, "22545b38-0dfb-46dd-909e-c4ad2fcab5a1", "6c603151-edf3-4785-a3b0-00aaa6607c1d", "d441887fb9c612b753545bdb3e6c803c43cd6b6f7ee7cbace6a4aaa51a0573e2"},
	{67, "d292dbd2-e231-4505-b3c1-18d7cd2e12a8", "c436b386-b875-4365-be8c-39398228e137", "a36876b433777854c483bf80ae57fd512e155d35e2e9f6b74e6c5f4481bebc80"},
	{68, "06ecd780-450f-4c29-a5ef-cc772c007b4b", "abe39ad8-c017-416b-8041-6f097e8fd50a", "face714e1e21d1093bf1eaba967a3a0ceb6875f70080be23703ba5668ff21cfa"},
	{69, "5febd4b1-cb81-42a3-acd9-9f9d98a8f347", "4c06e7ed-6f0c-46b8-adbf-c060f0233dae", "9780063dc164b7417c252bbdd033b1d1b9a56e13fbceb1faa0b9be4780dc13fe"},
	{70, "8ac3f398-a3c2-48e4-89e3-087c77c827e3", "16d56223-4081-47ae-9ecd-61eb9104c6e7", "fc578f382d123a416a691d13574d05eadca5faf090bda1453fb8f89fb5b36998"},
	{71, "86721b51-db3d-42f0-9ba2-7f208d461286", "ba7440e0-2f4e-49fa-8b90-ce25bc73474c", "0664b59380117b1aced18e0fb4587eaf745c6d8a35f33867b58de6e2fbfcc175"},
	{72, "19394d9c-1050-46ef-b931-5ef3123f12a0", "7646dbe7-bcbb-4760-926a-580c9ca6934a", "749e57e5eded5776703335b88e40adebca3884a6ec24a22a54eec891afc044d4"},
	{73, "da8ad421-b3bc-43ab-9f40-40a8181eb87d", "bbc8e3ac-b712-4507-8846-9b09e13717de", "354b89ea4939a9f9ed4d97c401e2c84f71c11228fd429404f60b680f96b3adc7"},
	{74, "49a62211-703b-4adf-abae-70cf2af78e7e", "f2168fbf-d7c9-4e96-8cc6-784bb1546083", "60df838e3e66d376e7c8c8de51c9ef630aa6aaa6b627823c9867f1da2fdca232"},
	{75, "de723ecd-9627-4336-a9db-58d96da9d052", "0374fd04-2b3c-4a7d-ac8c-e93ccaedc3ce", "fd1ff3da0a09dd66fc49bc2370602e1e346bd059eab4df1e97b057108f71bf0e"},
	{76, "6c36eb05-b27e-4564-b8b6-b95f8da8ccc4", "4de6d08d-b41e-4940-b777-a7b9d6d0370b", "cee57cd5d2526f855456b334d44c1166a99029dc9f1ec9233c8fe428e5efc25b"},
	{77, "6e189b3c-a168-4761-9beb-963902c33877", "43d9a893-ff56-41ca-b36c-ea77ef546d0c", "d85e2cfecf0346e207a848d2b76447ccc602afa050730936b2b1f56fac429cc2"},
	{78, "ac293c97-dedc-44e8-b1b9-cddcdc1b499c", "97f2f3d1-c20e-4f35-91d4-2e860a333962", "eda60e9bf669a9cfd74db2c409ba594afca37e0000a719c60bd5e5efab4430de"},
	{79, "1882bd1f-8e53-4a9b-8450-a91015664837", "b95620b8-9d13-416a-aeab-8576833a7e8c", "fcabc31eb52d1911f4e58831ffc0f6e22f6a82a119862414300fa82c5096a4b4"},
	{80, "5d5b9742-be47-4153-b8a6-39a5845b526f", "cb9b2b16-ebbe-4690-a1f4-0bbc6651174b", "bd93ef1993da034c6f745d004ea346ca93a5fb036d842e67070d6bb3387b72ca"},
	{81, "711b9a7d-fed4-4fa4-b7e7-3d719c1a64ac", "6ea21854-9dbd-44e1-92a0-ee9e0613a3a5", "d38ac0280cbf01dd1817433bd69c60f038a3074f26b41709bc78ecb7f5968f52"},
	{82, "b8a8d332-cc4b-4816-9a1a-7a347b2fa204", "500063f2-2f5d-4e82-9208-1e3ad92dfee5", "1519e7274c5650b52396f8531aa7fc611f386b3de141d95b7cde4370d950d43a"},
	{83, "0d8b2a3c-86e3-448c-9156-d7f59e40b11c", "6df02902-13bf-4b35-9f74-93dae719888c", "cc1c3481a3b2a84c429a64554a893fe1cfbade64ecc236aeb55ddbc811440b6d"},
	{84, "1bc185ad-0977-4b3f-bb1c-404bc37e94fa", "eca4d47b-5911-4f0f-8b45-89c1be775925", "c149fba74453b130803396f16de66fb9ca21b8c975c66a388d5619943e50fe7c"},
	{85, "adfd7e06-a2f5-4cce-b97c-4de767256ac9", "2a7b1169-7466-4561-85bf-faceee50b027", "50270369b79cf434ca36c667c1599671ba89c07b09f177cb1e3ecfadf2a21fdc"},
	{86, "b665cc75-73b4-4434-b5bb-cd35a8d0eb5e", "ce31c3da-f948-4dcf-b01c-8943b0f36d7f", "61474ed6c7588ce8c450e79cef239ce94f087d67e3d1cd94139249ee74b110ba"},
	{87, "3ff8fcf7-5ff3-4a18-b2ad-32cc8625043f", "2ab49378-9784-4ea9-a06a-b84ad255f238", "9a5598f800ac199ae2784b6518cfc7120a975841b15cdb8b24411dc071744933"},
}

// AuditOriginalCommandCodecs uses a READ ONLY MD transaction and exact original
// keys/hashes. It returns only counts, fixed IDs, hashes and finite guards.
func (r *ModelDevAcceptanceRepo) AuditOriginalCommandCodecs(ctx context.Context, md *sql.DB) (OriginalCodecAudit, error) {
	out := OriginalCodecAudit{Failures: []OriginalCodecFailure{}}
	ctx = appViewer.NewSystemViewerContext(ctx)
	if md == nil {
		return out, errors.New("ORIGINAL_MD_READ_ONLY_CONNECTION_REQUIRED")
	}
	tx, e := md.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return out, errors.New("ORIGINAL_MD_READ_ONLY_BEGIN_FAILED")
	}
	defer tx.Rollback()
	for _, w := range originalMDCodecScope {
		out.MDTotal++
		var op, spec, ih, actor string
		var accepted sql.NullTime
		var ir, sr []byte
		e = tx.QueryRowContext(ctx, `SELECT operation_id::text,spec_hash,intent_hash,actor,accepted_at,intent_canonical,snapshot_canonical FROM modeldev_executions WHERE tenant_id=$1::uuid AND execution_id=$2::uuid`, "64c5c35a-f554-432c-b4bc-7b4f36f08420", w.Execution).Scan(&op, &spec, &ih, &actor, &accepted, &ir, &sr)
		guard := ""
		if e != nil {
			guard = "ORIGINAL_ROW_UNAVAILABLE"
		} else if op != w.Operation || spec != w.Spec {
			guard = "ORIGINAL_IDENTITY_DIFFERS"
		} else {
			i, ie := cpup01.DecodeCanonicalIntent(ir)
			s, se := cpup01.DecodeCanonicalSnapshot(sr)
			if ie != nil {
				guard = "INTENT_NATIVE_DECODE"
			} else if se != nil {
				guard = "SNAPSHOT_NATIVE_DECODE"
			} else {
				a := cpup01.AdmissionEnvelope{TenantID: "64c5c35a-f554-432c-b4bc-7b4f36f08420", Actor: actor, OperationID: op, ExecutionID: w.Execution, Intent: i, IntentHash: ih, Snapshot: s, SpecHash: spec, AcceptedAt: accepted.Time}
				ci, cs, ce := a.CanonicalPayloads()
				if ce != nil || !accepted.Valid || !bytes.Equal(ci, ir) || !bytes.Equal(cs, sr) {
					guard = "ORIGINAL_ENVELOPE_BYTES"
				}
			}
		}
		if guard == "" {
			out.MDPass++
		} else {
			out.Failures = append(out.Failures, OriginalCodecFailure{"MD", w.Execution, w.Spec, guard})
		}
	}
	if tx.Commit() != nil {
		return out, errors.New("ORIGINAL_MD_READ_ONLY_COMMIT_FAILED")
	}
	for _, w := range originalGovCodecScope {
		out.GovTotal++
		row, e := r.entClient.Client().ModelDevAcceptance.Query().Where(modeldevacceptance.IDEQ(w.ID)).Only(ctx)
		guard := ""
		if e != nil {
			guard = "ORIGINAL_ROW_UNAVAILABLE"
		} else if row.TenantID == nil || *row.TenantID != 2 || row.ResourceTenantID != "64c5c35a-f554-432c-b4bc-7b4f36f08420" || row.OperationID != w.Operation || row.ExecutionID != w.Execution || row.ExecutionSpecHash != w.Spec {
			guard = "ORIGINAL_IDENTITY_DIFFERS"
		} else if _, e := modelDevAcceptanceFromRow(row); e != nil {
			guard = "NATIVE_PERSISTED_ACCEPTANCE_DECODE"
		}
		if guard == "" {
			out.GovPass++
		} else {
			out.Failures = append(out.Failures, OriginalCodecFailure{"GOV", w.Execution, w.Spec, guard})
		}
	}
	return out, nil
}
