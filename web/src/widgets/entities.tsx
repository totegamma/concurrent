import { Entity } from "@concurrent-world/client/dist/types/model/core"
import { Box, Button, Drawer, List, ListItem, ListItemButton, ListItemText, TextField, Typography } from "@mui/material"
import { forwardRef, useEffect, useState } from "react"
import { useApi } from "../context/apiContext"

export const Entities = forwardRef<HTMLDivElement>((props, ref): JSX.Element => {

    const { api } = useApi()

    const token = localStorage.getItem("JWT")
    const [entities, setEntities] = useState<Entity[]>([])
    const [newCCID, setNewCCID] = useState<string>('')
    const [selectedEntity, setSelectedEntity] = useState<Entity | null>(null)
    const [newTag, setNewTag] = useState<string>('')
    const [newScore, setNewScore] = useState<number>(0)

    const refresh = () => {
        api.getEntities().then(setEntities)
    }

    useEffect(() => {
        refresh()
    }, [])

    return (
        <div ref={ref} {...props}>
            <Box sx={{
                width: '100%'
            }}>
                <Typography>Entities</Typography>
                <Box sx={{ display: 'flex', gap: '10px' }}>
                    <TextField
                        label="CCID"
                        variant="outlined"
                        value={newCCID}
                        sx={{ flexGrow: 1 }}
                        onChange={(e) => {
                            setNewCCID(e.target.value)
                        }}
                    />
                    <Button
                        variant="contained"
                        onClick={(_) => {
                            api.createEntity(newCCID)
                        }}
                    >
                        Register
                    </Button>
                </Box>
                <List
                    disablePadding
                >
                    {entities.map((entity) => (
                        <ListItem key={entity.ccid}
                            disablePadding
                        >
                            <ListItemButton
                                onClick={() => {
                                    setNewTag(entity.tag)
                                    setNewScore(entity.score)
                                    setSelectedEntity(entity)
                                }}
                            >
                                <ListItemText primary={entity.ccid} secondary={`${entity.cdate}`} />
                                <ListItemText>{`${entity.tag}(${entity.score})`}</ListItemText>
                            </ListItemButton>
                        </ListItem>
                    ))}
                </List>
            </Box>
            <Drawer
                anchor="right"
                open={selectedEntity !== null}
                onClose={() => {
                    setSelectedEntity(null)
                }}
            >
                {selectedEntity && 
                <Box
                    width="50vw"
                    display="flex"
                    flexDirection="column"
                    gap={1}
                    padding={2}
                >
                    <Typography>{selectedEntity.ccid}</Typography>
                    <pre>{JSON.stringify(selectedEntity, null, 2)}</pre>
                    <TextField
                        label="new tag"
                        variant="outlined"
                        value={newTag}
                        sx={{ flexGrow: 1 }}
                        onChange={(e) => {
                            setNewTag(e.target.value)
                        }}
                    />
                    <TextField
                        label="new score"
                        variant="outlined"
                        value={newScore}
                        sx={{ flexGrow: 1 }}
                        onChange={(e) => {
                            setNewScore(Number(e.target.value))
                        }}
                    />
                    <Button
                        variant="contained"
                        onClick={(_) => {
                            if (!token) return
                            api.updateEntity({
                                ...selectedEntity,
                                tag: newTag,
                                score: newScore,
                            }).then(() => {
                                refresh()
                            })
                        }}
                    >
                        Update
                    </Button>
                    <Button
                        variant="contained"
                        onClick={(_) => {
                            if (!selectedEntity) return
                            api.deleteEntity(selectedEntity.ccid).then(() => {
                                setSelectedEntity(null)
                                refresh()
                            })
                        }}
                        color="error"
                    >
                        Delete
                    </Button>
                </Box>
                }
            </Drawer>
        </div>
    )
})

Entities.displayName = "Entities"

