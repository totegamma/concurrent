import { Box, Button, Drawer, List, ListItem, ListItemButton, ListItemText, TextField, Typography } from "@mui/material"
import { forwardRef, useEffect, useState } from "react"
import { getEntities, deleteEntity, createEntity } from "../util"
import { Entity } from "../model"

export const Entities = forwardRef<HTMLDivElement>((props, ref): JSX.Element => {

    const token = localStorage.getItem("JWT")
    const [entities, setEntities] = useState<Entity[]>([])
    const [newCCID, setNewCCID] = useState<string>('')
    const [selectedEntity, setSelectedEntity] = useState<Entity | null>(null)
    const [newRole, setNewRole] = useState<string>('')
    const [newScore, setNewScore] = useState<number>(0)

    useEffect(() => {
        getEntities().then(setEntities)
    }, [])

    return (
        <div ref={ref} {...props}>
            <Box sx={{ position: 'absolute', width: '100%' }}>
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
                            if (!token) return
                            createEntity(token, newCCID)
                        }}
                    >
                        Register
                    </Button>
                </Box>
                <List
                    disablePadding
                >
                    {entities.map((entity) => (
                        <ListItem key={entity.ccaddr}
                            disablePadding
                        >
                            <ListItemButton
                                onClick={() => {
                                    setNewRole(entity.role)
                                    setNewScore(entity.score)
                                    setSelectedEntity(entity)
                                }}
                            >
                                <ListItemText primary={entity.ccaddr} secondary={`${entity.cdate}`} />
                                <ListItemText>{`${entity.role}(${entity.score})`}</ListItemText>
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
                <Box
                    width="50vw"
                    display="flex"
                    flexDirection="column"
                    gap={1}
                    padding={2}
                >
                    <Typography>{selectedEntity?.ccaddr}</Typography>
                    <pre>{JSON.stringify(selectedEntity, null, 2)}</pre>
                    <TextField
                        label="new role"
                        variant="outlined"
                        value={newRole}
                        sx={{ flexGrow: 1 }}
                        onChange={(e) => {
                            setNewRole(e.target.value)
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
                        }}
                    >
                        Update
                    </Button>
                    <Button
                        variant="contained"
                        onClick={(_) => {
                            if (!token) return
                            if (!selectedEntity) return
                            deleteEntity(token, selectedEntity.ccaddr)
                            setSelectedEntity(null)
                        }}
                        color="error"
                    >
                        Delete
                    </Button>
                </Box>
            </Drawer>
        </div>
    )
})

Entities.displayName = "Entities"

